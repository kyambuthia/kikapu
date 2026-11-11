package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/payment/mpesa"
	"github.com/kyambuthia/kikapu/internal/payment/paystack"
	"github.com/kyambuthia/kikapu/internal/platform/config"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
	"github.com/kyambuthia/kikapu/internal/routes"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	mode := flag.String("mode", "api", "operation mode (api|worker)")
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

	if cfg.Mode != "api" {
		logger.Info("starting in worker mode", "mode", cfg.Mode)
		return
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

	idempotencyStore := checkout.NewIdempotencyStore()
	checkoutSvc := checkout.NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore)

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

	handler := httpx.LoggingMiddleware(logger)(httpx.RequestIDMiddleware(mux))

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
