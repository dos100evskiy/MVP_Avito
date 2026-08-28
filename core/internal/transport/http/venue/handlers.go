// Package venue содержит хендлеры Venue API (/api/v1/venue/...) —
// сторона заведения: управление меню и обработка своих заказов.
// Все роуты этого пакета защищены middleware.VenueAuth (см. router.go).
//
// Типы запросов/ответов сгенерированы из api/openapi/core-venue-api.yaml
// через oapi-codegen (см. types.gen.go рядом, make generate) и живут в этом
// же пакете. Обратите внимание: venue.Order (в отличие от client.Order) не
// содержит поля Items — ровно как объявлено в спеке для Venue API; раньше,
// на ручных DTO, тут был общий OrderDTO с Items, из-за чего ответ
// GET /venue/orders фактически отдавал лишнее "items":null, которого не
// было в OpenAPI-контракте — молчаливое расхождение спеки и кода, которое
// кодогенерация как раз и устраняет.
package venue

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/avito/kuhnya/core/internal/domain"
	httpapi "github.com/avito/kuhnya/core/internal/transport/http"
	"github.com/avito/kuhnya/core/internal/transport/http/middleware"
)

// MenuUseCase и OrderUseCase — минимальные интерфейсы, покрывающие ровно то,
// что реально вызывают хендлеры этого пакета (см. аналогичное пояснение в
// client/handlers.go). Их удовлетворяют *usecase.VenueMenuUseCase и
// *usecase.OrderUseCase без единой правки на их стороне.
type MenuUseCase interface {
	CreateItem(ctx context.Context, item domain.MenuItem) (string, error)
	SetAvailability(ctx context.Context, itemID string, available bool) error
}

type OrderUseCase interface {
	ListVenueOrders(ctx context.Context, venueID string, status domain.OrderStatus, limit, offset int) ([]domain.Order, error)
	ChangeStatus(ctx context.Context, orderID string, to domain.OrderStatus, changedBy string) error
}

type Handlers struct {
	menu   MenuUseCase
	orders OrderUseCase
}

func New(menu MenuUseCase, orders OrderUseCase) *Handlers {
	return &Handlers{menu: menu, orders: orders}
}

func (h *Handlers) Mount(r chi.Router) {
	r.Post("/menu-items", h.createMenuItem)
	r.Patch("/menu-items/{itemId}/availability", h.setAvailability)

	r.Get("/orders", h.listOrders)
	r.Post("/orders/{orderId}/accept", h.acceptOrder)
	r.Post("/orders/{orderId}/reject", h.rejectOrder)
	r.Patch("/orders/{orderId}/status", h.updateOrderStatus)
}

func (h *Handlers) createMenuItem(w http.ResponseWriter, r *http.Request) {
	venueID, _ := middleware.VenueIDFromContext(r.Context())

	var req CreateMenuItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	currency := "RUB"
	if req.Currency != nil && *req.Currency != "" {
		currency = *req.Currency
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}

	id, err := h.menu.CreateItem(r.Context(), domain.MenuItem{
		VenueID:      venueID,
		CategoryID:   req.CategoryId,
		Name:         req.Name,
		Description:  description,
		PriceKopecks: req.PriceKopecks,
		Currency:     currency,
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handlers) setAvailability(w http.ResponseWriter, r *http.Request) {
	var req AvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	available := req.IsAvailable != nil && *req.IsAvailable
	if err := h.menu.SetAvailability(r.Context(), chi.URLParam(r, "itemId"), available); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) listOrders(w http.ResponseWriter, r *http.Request) {
	venueID, _ := middleware.VenueIDFromContext(r.Context())
	status := domain.OrderStatus(r.URL.Query().Get("status"))

	orders, err := h.orders.ListVenueOrders(r.Context(), venueID, status, 20, 0)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	dtos := make([]Order, 0, len(orders))
	for _, o := range orders {
		dtos = append(dtos, toOrder(o))
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (h *Handlers) acceptOrder(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, domain.OrderStatusAccepted)
}

func (h *Handlers) rejectOrder(w http.ResponseWriter, r *http.Request) {
	// reason из тела запроса пока не персистится отдельным полем (см. допущения
	// в README) — декодируем ради валидности запроса, дальше не используется;
	// для расширения естественное место — колонка order_status_history.reason.
	var req RejectOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	h.transition(w, r, domain.OrderStatusCancelled)
}

func (h *Handlers) updateOrderStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Status == nil {
		http.Error(w, `{"error":"status is required"}`, http.StatusBadRequest)
		return
	}
	h.transition(w, r, domain.OrderStatus(*req.Status))
}

func (h *Handlers) transition(w http.ResponseWriter, r *http.Request, to domain.OrderStatus) {
	orderID := chi.URLParam(r, "orderId")
	if err := h.orders.ChangeStatus(r.Context(), orderID, to, "VENUE"); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toOrder(o domain.Order) Order {
	status := string(o.Status)
	return Order{
		Id:           httpapi.Ptr(o.ID),
		VenueId:      httpapi.Ptr(o.VenueID),
		Status:       &status,
		TotalKopecks: httpapi.Ptr(o.TotalKopecks),
		Currency:     httpapi.Ptr(o.Currency),
		CustomerRef:  httpapi.Ptr(o.CustomerRef),
		CreatedAt:    httpapi.Ptr(o.CreatedAt),
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
