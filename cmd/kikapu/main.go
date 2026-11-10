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

	"github.com/kyambuthia/kikapu/internal/platform/config"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	mode := flag.String("mode", "api", "operation mode (api|worker)")
	showHelp := flag.Bool("help", false, "show help")
	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

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

	mux := http.NewServeMux()
	
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

	handler := httpx.LoggingMiddleware(mux)

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("starting server", "addr", cfg.Addr, "version", version, "commit", commit)
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
