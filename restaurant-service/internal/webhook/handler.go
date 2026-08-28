// Package webhook — приёмник push-нотификаций от core-сервиса о новых заказах
// (см. core/internal/infrastructure/webhook — там же отправка).
package webhook

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/avito/kuhnya/restaurant-service/internal/kitchen"
)

type payload struct {
	OrderID string `json:"orderId"`
	VenueID string `json:"venueId"`
	Status  string `json:"status"`
}

func NewHandler(emulator *kitchen.Emulator, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, `{"error":"invalid payload"}`, http.StatusBadRequest)
			return
		}
		log.Info("webhook: new order received", "order_id", p.OrderID)
		emulator.HandleNewOrder(p.OrderID)
		w.WriteHeader(http.StatusAccepted)
	}
}
