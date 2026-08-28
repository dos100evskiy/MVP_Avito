package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/avito/kuhnya/core/internal/domain"
	clienthttp "github.com/avito/kuhnya/core/internal/transport/http/client"
	"github.com/avito/kuhnya/core/internal/transport/http/middleware"
	venuehttp "github.com/avito/kuhnya/core/internal/transport/http/venue"
)

type Deps struct {
	Log        *slog.Logger
	VenueRepo  domain.VenueRepository
	ClientHTTP *clienthttp.Handlers
	VenueHTTP  *venuehttp.Handlers
}

// NewRouter собирает дерево роутов. Client API и Venue API разнесены
// по разным префиксам и разным моделям авторизации (см. CJM в README):
// /api/v1/client — без авторизации (по ТЗ), /api/v1/venue — X-API-Key.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logging(d.Log))
	r.Use(middleware.Recover(d.Log))

	r.Get("/health", healthHandler)

	r.Route("/api/v1/client", func(r chi.Router) {
		d.ClientHTTP.Mount(r)
	})

	r.Route("/api/v1/venue", func(r chi.Router) {
		r.Use(middleware.VenueAuth(d.VenueRepo))
		d.VenueHTTP.Mount(r)
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
