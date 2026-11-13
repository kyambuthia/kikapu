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
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kyambuthia/kikapu/internal/admin"
	"github.com/kyambuthia/kikapu/internal/auth"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/fulfillment"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/migrate"
	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/payment/mpesa"
	"github.com/kyambuthia/kikapu/internal/payment/paystack"
	"github.com/kyambuthia/kikapu/internal/platform/config"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
	"github.com/kyambuthia/kikapu/internal/region"
	"github.com/kyambuthia/kikapu/internal/routes"
	"github.com/kyambuthia/kikapu/internal/search"
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

	shutdownTracer, err := obs.InitTracer(context.Background(), "kikapu", version)
	if err != nil {
		logger.Warn("failed to initialize tracer", "error", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTracer(ctx); err != nil {
				logger.Warn("tracer shutdown error", "error", err)
			}
		}()
	}

	var (
		db              *sql.DB
		eventOutbox     events.Outbox
		repo            catalog.Repository
		cartStore       cart.Store
		adminProducts   admin.ProductRepository
		adminStock      admin.StockRepository
		adminOrderStore admin.OrderLister
		fulfillmentSvc  *fulfillment.Service
		searchStore     *search.PostgresStore
		authStore       interface {
			auth.KeyStore
			Seed(name, rawKey string, scopes []string) (*auth.Key, error)
		}
	)

	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		var err error
		db, err = openPostgres(databaseURL)
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		eventOutbox = outbox.NewPostgresOutbox(db)
		repo = catalog.NewPostgresStore(db)
		cartStore = cart.NewPostgresStore(db)
		adminProducts = admin.NewPostgresProductRepository(db)
		adminStock = admin.NewPostgresStockRepository(db)
		adminOrderStore = admin.NewPostgresOrderLister(db)
		fulfillmentSvc = fulfillment.NewService(
			fulfillment.NewPostgresRepository(db),
			&orderFetcherAdapter{store: order.NewPostgresStore(db)},
		)
		authStore = auth.NewPostgresStore(db)
		searchStore = search.NewPostgresStore(db)
		logger.Info("using PostgreSQL persistence")
	} else {
		eventOutbox = outbox.NewInMemoryOutbox()
		cartStore = cart.NewInMemoryStore()
		adminProducts = admin.NewMemoryProductRepository()
		adminStock = admin.NewMemoryStockRepository()
		adminOrderStore = admin.NewMemoryOrderLister()
		authStore = auth.NewInMemoryStore()
	}

	subscriberStore := worker.NewInMemorySubscriberStore()
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	deliverer := worker.NewDeliverer(httpClient, eventOutbox, subscriberStore)

	if cfg.Mode == "worker" {
		runWorker(logger, deliverer)
		return
	}

	if db == nil {
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

		repo = catalog.NewInMemoryRepository(products)
		logger.Info("DATABASE_URL not set, using in-memory repositories")
	}

	regionRepo := region.NewInMemoryRepository(defaultRegions())
	channelRepo := channel.NewInMemoryRepository(defaultChannels())

	inventoryStore := inventory.NewMemoryStore(nil)

	paymentProvider, err := buildPaymentProvider(cfg)
	if err != nil {
		logger.Error("failed to create payment provider", "provider", cfg.PaymentProvider, "error", err)
		os.Exit(1)
	}

	idempotencyStore := checkout.NewIdempotencyStore()
	checkoutSvc := checkout.NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	go deliverer.Run(workerCtx, 5*time.Second)

	var paystackWebhook http.Handler
	if psClient, ok := paymentProvider.(*paystack.Client); ok {
		paystackWebhook = paystack.NewWebhookHandler(psClient, paystack.NewInMemoryEventStore(), logger)
	}

	var mpesaCallback http.Handler
	if cfg.PaymentProvider == "mpesa" || cfg.MpesaAllowedCIDRs != "" {
		cbHandler, err := mpesa.NewCallbackHandler(
			mpesa.NewInMemoryCallbackStore(),
			parseCSV(cfg.MpesaAllowedCIDRs),
			logger,
			false,
		)
		if err != nil {
			logger.Error("failed to create M-Pesa callback handler", "error", err)
			os.Exit(1)
		}
		mpesaCallback = cbHandler
	}

	if adminKey := os.Getenv("KIKAPU_ADMIN_KEY"); adminKey != "" {
		if _, err := authStore.Seed("bootstrap-admin", adminKey, []string{"admin"}); err != nil {
			logger.Error("failed to seed admin API key", "error", err)
			os.Exit(1)
		}
	}

	mux := buildAPIMux(apiDeps{
		db:              db,
		catalogRepo:     repo,
		regionRepo:      regionRepo,
		channelRepo:     channelRepo,
		cartStore:       cartStore,
		checkoutSvc:     checkoutSvc,
		adminProducts:   adminProducts,
		adminStock:      adminStock,
		adminOrderStore: adminOrderStore,
		fulfillmentSvc:  fulfillmentSvc,
		searchStore:     searchStore,
		authStore:       authStore,
		paystackWebhook: paystackWebhook,
		mpesaCallback:   mpesaCallback,
	})
	handler := httpx.LoggingMiddleware(logger)(httpx.RequestIDMiddleware(obs.MetricsMiddleware(mux)))

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

func runWorker(logger *slog.Logger, deliverer *worker.Deliverer) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info("starting in worker mode", "version", version, "commit", commit)
	go deliverer.Run(ctx, 5*time.Second)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down worker")
	cancel()
}

func buildPaymentProvider(cfg *config.Config) (payment.Provider, error) {
	switch cfg.PaymentProvider {
	case "paystack":
		return paystack.New(paystack.Config{
			BaseURL:   cfg.PaystackBaseURL,
			SecretKey: cfg.PaystackSecretKey,
		})
	case "mpesa":
		return mpesa.New(mpesa.Config{
			BaseURL:        cfg.MpesaBaseURL,
			ConsumerKey:    cfg.MpesaConsumerKey,
			ConsumerSecret: cfg.MpesaConsumerSecret,
			ShortCode:      cfg.MpesaShortCode,
			Passkey:        cfg.MpesaPasskey,
			CallbackURL:    cfg.MpesaCallbackURL,
		})
	default:
		return payment.NewFakeProvider(payment.NoFailure), nil
	}
}

type apiDeps struct {
	db              *sql.DB
	catalogRepo     catalog.Repository
	regionRepo      region.Repository
	channelRepo     channel.Repository
	cartStore       cart.Store
	checkoutSvc     *checkout.Service
	adminProducts   admin.ProductRepository
	adminStock      admin.StockRepository
	adminOrderStore admin.OrderLister
	fulfillmentSvc  *fulfillment.Service
	searchStore     *search.PostgresStore
	authStore       auth.KeyStore
	paystackWebhook http.Handler
	mpesaCallback   http.Handler
}

func buildAPIMux(deps apiDeps) *http.ServeMux {
	mux := http.NewServeMux()

	routes.NewCatalogHandlerWithFilters(deps.catalogRepo, deps.regionRepo, deps.channelRepo).RegisterRoutes(mux)
	routes.NewRegionHandler(deps.regionRepo).RegisterRoutes(mux)
	routes.NewChannelHandler(deps.channelRepo).RegisterRoutes(mux)
	routes.NewCartHandler(deps.cartStore, deps.checkoutSvc).RegisterRoutes(mux)
	if deps.searchStore != nil {
		routes.NewSearchHandler(deps.searchStore).RegisterRoutes(mux)
	}

	if deps.adminProducts != nil && deps.adminStock != nil {
		adminMux := http.NewServeMux()
		adminHandler := admin.NewHandler(deps.adminProducts, deps.adminStock)
		if deps.adminOrderStore != nil {
			adminHandler.SetOrderStore(deps.adminOrderStore)
		}
		if deps.fulfillmentSvc != nil {
			adminHandler.SetFulfillmentService(deps.fulfillmentSvc)
		}
		adminHandler.RegisterRoutes(adminMux)

		var adminRoute http.Handler = adminMux
		if deps.authStore != nil {
			adminRoute = auth.RequireScope(adminMux, deps.authStore, "admin")
		}
		mux.Handle("/v1/admin/", adminRoute)
	}

	if deps.authStore != nil {
		authMux := http.NewServeMux()
		createKey := auth.CreateKeyHandler(deps.authStore)
		listKeys := auth.ListKeysHandler(deps.authStore)
		revokeKey := auth.RevokeKeyHandler(deps.authStore)

		authMux.HandleFunc("/v1/auth/keys", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				listKeys(w, r)
			case http.MethodPost:
				createKey(w, r)
			default:
				httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
			}
		})
		authMux.HandleFunc("DELETE /v1/auth/keys/{id}", revokeKey)

		mux.Handle("/v1/auth/", auth.RequireScope(authMux, deps.authStore, "admin"))
	}

	if deps.paystackWebhook != nil {
		mux.Handle("/v1/webhooks/paystack", deps.paystackWebhook)
	}
	if deps.mpesaCallback != nil {
		mux.Handle("/v1/webhooks/mpesa", deps.mpesaCallback)
	}

	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := deps.db.PingContext(ctx); err != nil {
				httpx.WriteError(w, http.StatusServiceUnavailable, "not_ready", "database unavailable", httpx.RequestID(r))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"version": version,
			"commit":  commit,
		})
	})

	return mux
}

func openPostgres(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

type orderFetcherAdapter struct {
	store *order.PostgresStore
}

func (a *orderFetcherAdapter) Get(ctx context.Context, id string) (*fulfillment.OrderInfo, error) {
	o, err := a.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	lines := make([]fulfillment.OrderLine, 0, len(o.Lines))
	for _, l := range o.Lines {
		lines = append(lines, fulfillment.OrderLine{
			VariantID: l.VariantID,
			Quantity:  l.Quantity,
		})
	}
	return &fulfillment.OrderInfo{
		ID:     o.ID,
		Status: string(o.Status),
		Lines:  lines,
	}, nil
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func defaultRegions() []region.Region {
	return []region.Region{
		{
			ID:           "ke",
			Name:         "Kenya",
			Currency:     "KES",
			Countries:    []string{"KE"},
			TaxRateBPS:   1600,
			TaxInclusive: true,
		},
		{
			ID:           "us",
			Name:         "United States",
			Currency:     "USD",
			Countries:    []string{"US"},
			TaxRateBPS:   0,
			TaxInclusive: false,
		},
	}
}

func defaultChannels() []channel.SalesChannel {
	return []channel.SalesChannel{
		{
			ID:      "web",
			Name:    "Web Storefront",
			Slug:    "web",
			Regions: []string{"ke", "us"},
		},
	}
}
