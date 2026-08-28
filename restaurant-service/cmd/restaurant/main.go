package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/avito/kuhnya/restaurant-service/internal/config"
	"github.com/avito/kuhnya/restaurant-service/internal/coreclient"
	"github.com/avito/kuhnya/restaurant-service/internal/kitchen"
	"github.com/avito/kuhnya/restaurant-service/internal/webhook"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	core := coreclient.New(cfg.CoreAPIURL, cfg.CoreAPIKey)
	emulator := kitchen.New(core, log)

	mux := http.NewServeMux()
	mux.HandleFunc("/internal/orders", webhook.NewHandler(emulator, log))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stopPoller := make(chan struct{})
	if cfg.PollEnabled {
		go emulator.StartPoller(5*time.Second, stopPoller)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("restaurant-service starting", "port", cfg.HTTPPort, "core_api", cfg.CoreAPIURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
		}
	}()

	<-ctx.Done()
	close(stopPoller)
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
