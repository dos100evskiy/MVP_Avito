package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/avito/kuhnya/core/internal/config"
	"github.com/avito/kuhnya/core/internal/infrastructure/webhook"
	"github.com/avito/kuhnya/core/internal/logger"
	"github.com/avito/kuhnya/core/internal/repository/postgres"
	clienthttp "github.com/avito/kuhnya/core/internal/transport/http/client"
	"github.com/avito/kuhnya/core/internal/transport/http/server"
	venuehttp "github.com/avito/kuhnya/core/internal/transport/http/venue"
	"github.com/avito/kuhnya/core/internal/usecase"
)

func main() {
	log := logger.New()

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", "error", err)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "error", err)
		return
	}
	defer pool.Close()

	venueRepo := postgres.NewVenueRepo(pool)
	menuRepo := postgres.NewMenuRepo(pool)
	orderRepo := postgres.NewOrderRepo(pool)
	txManager := postgres.NewTxManager(pool)
	notifier := webhook.New(log)

	catalogUC := usecase.NewCatalogUseCase(venueRepo, menuRepo)
	venueMenuUC := usecase.NewVenueMenuUseCase(menuRepo)
	orderUC := usecase.NewOrderUseCase(venueRepo, menuRepo, orderRepo, txManager, notifier)

	router := server.NewRouter(server.Deps{
		Log:        log,
		VenueRepo:  venueRepo,
		ClientHTTP: clienthttp.New(catalogUC, orderUC),
		VenueHTTP:  venuehttp.New(venueMenuUC, orderUC),
	})

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http server starting", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}
}
