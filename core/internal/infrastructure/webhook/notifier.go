// Package webhook — реализация usecase.OrderNotifier: push-нотификация
// заведения о новом заказе на его callback_url (см. CJM заведения: "push" вариант
// в дополнение к поллингу GET /venue/orders?status=CREATED).
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/avito/kuhnya/core/internal/domain"
)

type Notifier struct {
	client *http.Client
	log    *slog.Logger
}

func New(log *slog.Logger) *Notifier {
	return &Notifier{
		client: &http.Client{Timeout: 3 * time.Second},
		log:    log,
	}
}

type newOrderPayload struct {
	OrderID string `json:"orderId"`
	VenueID string `json:"venueId"`
	Status  string `json:"status"`
}

// NotifyNewOrder — best-effort, не блокирует и не возвращает ошибку вызывающему
// коду: если у заведения не настроен callback_url или он недоступен, заведение
// всё равно узнает о заказе через поллинг Venue API.
func (n *Notifier) NotifyNewOrder(ctx context.Context, venue domain.Venue, order domain.Order) {
	if venue.CallbackURL == "" {
		return
	}
	go func() {
		reqCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		body, _ := json.Marshal(newOrderPayload{OrderID: order.ID, VenueID: order.VenueID, Status: string(order.Status)})
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, venue.CallbackURL, bytes.NewReader(body))
		if err != nil {
			n.log.Warn("webhook: build request failed", "venue_id", venue.ID, "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := n.client.Do(req)
		if err != nil {
			n.log.Warn("webhook: delivery failed, venue will fall back to polling", "venue_id", venue.ID, "error", err)
			return
		}
		defer resp.Body.Close()
	}()
}
