package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avito/kuhnya/core/internal/domain"
)

// errorResponse — единый формат ошибок API, отдаётся по всем эндпоинтам.
type errorResponse struct {
	Error   string      `json:"error"`
	Details interface{} `json:"details,omitempty"`
}

// WriteError маппит ошибки domain-слоя в HTTP-статусы. Это единственное место
// в проекте, где ошибка "знает" про HTTP-код — usecase-слой полностью его не знает.
func WriteError(w http.ResponseWriter, err error) {
	var unavailable *domain.ErrItemsUnavailable

	switch {
	case errors.As(err, &unavailable):
		writeJSON(w, http.StatusConflict, errorResponse{
			Error:   "some items are unavailable",
			Details: unavailable.Items,
		})
	case errors.Is(err, domain.ErrVenueNotFound),
		errors.Is(err, domain.ErrMenuItemNotFound),
		errors.Is(err, domain.ErrOrderNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrItemsFromDiffVenues),
		errors.Is(err, domain.ErrEmptyOrder),
		errors.Is(err, domain.ErrInvalidTransition):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
