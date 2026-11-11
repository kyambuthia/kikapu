package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"strings"
	"syscall"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/idempotency"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/migrate"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/payment/mpesa"
	"github.com/kyambuthia/kikapu/internal/payment/paystack"
	"github.com/kyambuthia/kikapu/internal/platform/config"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
	"github.com/kyambuthia/kikapu/internal/routes"
	"github.com/kyambuthia/kikapu/internal/worker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	mode := flag.String("mode", "api", "operation mode (api|worker|migrate)")
	migrationsDir := flag.String("migrations", "migrations", "migrations directory")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if *addr != ":8080" {
		cfg.Addr = *addr
	}
	if *mode != "api" {
		cfg.Mode = *mode
	}

	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	} else if cfg.LogLevel == "warn" {
		level = slog.LevelWarn
	} else if cfg.LogLevel == "error" {
		level = slog.LevelError
	}
	logger := obs.NewLogger(level)

	if cfg.Mode == "migrate" {
		databaseURL := os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			fmt.Fprintln(os.Stderr, "DATABASE_URL environment variable is required for migration")
			os.Exit(1)
		}

		logger.Info("running migrations", "migrations_dir", *migrationsDir)

		if err := migrate.Run(databaseURL, *migrationsDir); err != nil {
			logger.Error("migration failed", "error", err)
			os.Exit(1)
		}

		logger.Info("migrations completed successfully")
		return
	}

	if cfg.Mode != "api" {
		logger.Info("starting in worker mode", "mode", cfg.Mode)
		return
	}

	var db *sql.DB
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		var err error
		db, err = sql.Open("postgres", databaseURL)
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	}

	var idempotencyStore idempotency.Store
	if db != nil {
		idempotencyStore = idempotency.NewPostgresStore(db)
	} else {
		idempotencyStore = &inMemoryIdempotencyStore{records: make(map[string]*inMemoryIdempotencyRecord)}
	}

	productData, err := os.ReadFile("internal/catalog/testdata/products.json")
	if err != nil {
		logger.Error("failed to read products data", "error", err)
		os.Exit(1)
	}

	products, err := catalog.LoadProductsFromJSON(productData)
	if err != nil {
		logger.Error("failed to load products", "error", err)
		os.Exit(1)
	}

	if err := catalog.ValidateProducts(products); err != nil {
		logger.Error("invalid products data", "error", err)
		os.Exit(1)
	}

	repo := catalog.NewInMemoryRepository(products)
	catalogHandler := routes.NewCatalogHandler(repo)

	cartStore := cart.NewInMemoryStore()
	inventoryStore := inventory.NewMemoryStore(nil)

	var paymentProvider payment.Provider
	switch cfg.PaymentProvider {
	case "paystack":
		psClient, err := paystack.New(paystack.Config{
			BaseURL:   cfg.PaystackBaseURL,
			SecretKey: cfg.PaystackSecretKey,
		})
		if err != nil {
			logger.Error("failed to create Paystack client", "error", err)
			os.Exit(1)
		}
		paymentProvider = psClient
	case "mpesa":
		mpesaClient, err := mpesa.New(mpesa.Config{
			BaseURL:        cfg.MpesaBaseURL,
			ConsumerKey:    cfg.MpesaConsumerKey,
			ConsumerSecret: cfg.MpesaConsumerSecret,
			ShortCode:      cfg.MpesaShortCode,
			Passkey:        cfg.MpesaPasskey,
			CallbackURL:    cfg.MpesaCallbackURL,
		})
		if err != nil {
			logger.Error("failed to create M-Pesa client", "error", err)
			os.Exit(1)
		}
		paymentProvider = mpesaClient
	default:
		paymentProvider = payment.NewFakeProvider(payment.NoFailure)
	}

	eventOutbox := outbox.NewInMemoryOutbox()
	_ = idempotencyStore
	checkoutIdempotencyStore := checkout.NewIdempotencyStore()
	checkoutSvc := checkout.NewService(cartStore, inventoryStore, paymentProvider, checkoutIdempotencyStore, eventOutbox)

	subscriberStore := worker.NewInMemorySubscriberStore()
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	deliverer := worker.NewDeliverer(httpClient, eventOutbox, subscriberStore)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	go deliverer.Run(workerCtx, 5*time.Second)

	if pgStore, ok := idempotencyStore.(*idempotency.PostgresStore); ok {
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					if err := pgStore.Cleanup(workerCtx); err != nil {
						logger.Error("idempotency cleanup failed", "error", err)
					}
				}
			}
		}()
	}

	cartHandler := routes.NewCartHandler(cartStore, checkoutSvc)
	
	mux := http.NewServeMux()
	
	catalogHandler.RegisterRoutes(mux)
	cartHandler.RegisterRoutes(mux)
	
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"version": version,
			"commit":  commit,
		})
	})
	
	handler := obs.NewMiddleware(obs.MiddlewareOptions{
		Logger:         logger,
		RequestTracker: requestTracker,
	})(httpx.RequestIDMiddleware(mux))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("starting server", "version", version, "commit", commit, "addr", cfg.Addr, "mode", cfg.Mode)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	workerCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

type inMemoryIdempotencyRecord struct {
	requestHash string
	status      int
	response    []byte
	createdAt   time.Time
	expiresAt   time.Time
}

type inMemoryIdempotencyStore struct {
	mu      sync.RWMutex
	records map[string]*inMemoryIdempotencyRecord
}

func (s *inMemoryIdempotencyStore) Begin(ctx context.Context, key, requestHash string) (idempotency.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, ok := s.records[key]; ok {
		if time.Now().After(rec.expiresAt) {
			delete(s.records, key)
			s.records[key] = &inMemoryIdempotencyRecord{
				requestHash: requestHash,
				createdAt:   time.Now(),
				expiresAt:   time.Now().Add(24 * time.Hour),
			}
			return idempotency.Record{Key: key, RequestHash: requestHash}, false, nil
		}
		if rec.requestHash != requestHash {
			return idempotency.Record{}, false, idempotency.ErrConflict
		}
		if rec.response == nil {
			return idempotency.Record{}, false, idempotency.ErrInFlight
		}
		return idempotency.Record{
			Key:         key,
			RequestHash: requestHash,
			Status:      rec.status,
			Response:    rec.response,
			CreatedAt:   rec.createdAt,
			ExpiresAt:   rec.expiresAt,
		}, true, nil
	}

	s.records[key] = &inMemoryIdempotencyRecord{
		requestHash: requestHash,
		createdAt:   time.Now(),
		expiresAt:   time.Now().Add(24 * time.Hour),
	}
	return idempotency.Record{Key: key, RequestHash: requestHash}, false, nil
}

func (s *inMemoryIdempotencyStore) Finish(ctx context.Context, key string, status int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, ok := s.records[key]; ok {
		rec.status = status
		rec.response = body
	}
	return nil
}

func (s *inMemoryIdempotencyStore) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, rec := range s.records {
		if now.After(rec.expiresAt) {
			delete(s.records, key)
		}
	}
	return nil
}
