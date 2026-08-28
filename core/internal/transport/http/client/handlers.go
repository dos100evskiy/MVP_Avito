// Package client содержит хендлеры Client API (/api/v1/client/...) —
// сторона обычного пользователя площадки: просмотр заведений/меню, заказы.
//
// Типы запросов/ответов (Venue, MenuItem, Order, CreateOrderRequest и т.д.)
// сгенерированы из api/openapi/core-client-api.yaml через oapi-codegen
// (см. types.gen.go и oapi-codegen-config.yaml рядом, make generate) —
// они живут в этом же пакете, поэтому используются без префикса.
package client

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/avito/kuhnya/core/internal/domain"
	httpapi "github.com/avito/kuhnya/core/internal/transport/http"
	"github.com/avito/kuhnya/core/internal/usecase"
)

// CatalogUseCase и OrderUseCase — минимальные интерфейсы, покрывающие ровно
// то, что реально вызывают хендлеры этого пакета (interface segregation).
// Их удовлетворяют конкретные *usecase.CatalogUseCase/*usecase.OrderUseCase
// без единой правки на их стороне (structural typing) — интерфейсы объявлены
// здесь, на стороне потребителя, специально ради этого: в handlers_test.go
// вместо них подставляется лёгкий мок, без поднятия БД/usecase-слоя целиком.
type CatalogUseCase interface {
	ListVenues(ctx context.Context, city string, limit, offset int) ([]domain.Venue, error)
	GetVenueMenu(ctx context.Context, venueID string) (*usecase.VenueMenu, error)
}

type OrderUseCase interface {
	CreateOrder(ctx context.Context, venueID, customerRef string, items []domain.NewOrderItemInput) (*domain.Order, error)
	GetOrder(ctx context.Context, id string) (*domain.Order, error)
	CancelOrder(ctx context.Context, orderID string) error
}

type Handlers struct {
	catalog CatalogUseCase
	orders  OrderUseCase
}

func New(catalog CatalogUseCase, orders OrderUseCase) *Handlers {
	return &Handlers{catalog: catalog, orders: orders}
}

func (h *Handlers) Mount(r chi.Router) {
	r.Get("/venues", h.listVenues)
	r.Get("/venues/{venueId}/menu", h.getVenueMenu)
	r.Post("/orders", h.createOrder)
	r.Get("/orders/{orderId}", h.getOrder)
	r.Post("/orders/{orderId}/cancel", h.cancelOrder)
}

func (h *Handlers) listVenues(w http.ResponseWriter, r *http.Request) {
	city := r.URL.Query().Get("city")
	venues, err := h.catalog.ListVenues(r.Context(), city, 20, 0)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	dtos := make([]Venue, 0, len(venues))
	for _, v := range venues {
		dtos = append(dtos, toVenue(v))
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (h *Handlers) getVenueMenu(w http.ResponseWriter, r *http.Request) {
	venueID := chi.URLParam(r, "venueId")
	menu, err := h.catalog.GetVenueMenu(r.Context(), venueID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}

	categories := make([]Category, 0, len(menu.Categories))
	for _, c := range menu.Categories {
		categories = append(categories, Category{Id: httpapi.Ptr(c.ID), Name: httpapi.Ptr(c.Name)})
	}
	items := make([]MenuItem, 0, len(menu.Items))
	for _, it := range menu.Items {
		items = append(items, toMenuItem(it))
	}
	venue := toVenue(menu.Venue)
	writeJSON(w, http.StatusOK, VenueMenu{
		Venue:      &venue,
		Categories: &categories,
		Items:      &items,
	})
}

func (h *Handlers) createOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	// TODO: подключить validator.Struct(req) вместо ручных проверок ниже,
	// как только будет зашит go-playground/validator (см. go.mod / план).
	// required-поля спеки (venueId/customerRef/items) уже отражены в самом
	// сгенерированном типе не-указателями, но пустая строка/срез всё ещё
	// технически валидны с точки зрения JSON-декодера — бизнес-валидацию
	// на "не пусто" всё равно делаем сами.
	if req.VenueId == "" || len(req.Items) == 0 {
		http.Error(w, `{"error":"venueId and items are required"}`, http.StatusBadRequest)
		return
	}

	items := make([]domain.NewOrderItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, domain.NewOrderItemInput{MenuItemID: it.MenuItemId, Quantity: it.Quantity})
	}

	order, err := h.orders.CreateOrder(r.Context(), req.VenueId, req.CustomerRef, items)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toOrder(*order))
}

func (h *Handlers) getOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.orders.GetOrder(r.Context(), chi.URLParam(r, "orderId"))
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOrder(*order))
}

func (h *Handlers) cancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderId")
	if err := h.orders.CancelOrder(r.Context(), orderID); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- маппинг domain -> сгенерированные из OpenAPI типы ---

func toVenue(v domain.Venue) Venue {
	return Venue{Id: httpapi.Ptr(v.ID), Name: httpapi.Ptr(v.Name), City: httpapi.Ptr(v.City)}
}

func toMenuItem(it domain.MenuItem) MenuItem {
	m := MenuItem{
		Id:           httpapi.Ptr(it.ID),
		Name:         httpapi.Ptr(it.Name),
		PriceKopecks: httpapi.Ptr(it.PriceKopecks),
		Currency:     httpapi.Ptr(it.Currency),
		IsAvailable:  httpapi.Ptr(it.IsAvailable),
	}
	if it.Description != "" {
		m.Description = httpapi.Ptr(it.Description)
	}
	if it.CategoryID != nil {
		m.CategoryId = httpapi.Ptr(*it.CategoryID)
	}
	return m
}

func toOrder(o domain.Order) Order {
	items := make([]OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, OrderItem{
			MenuItemId:   httpapi.Ptr(it.MenuItemID),
			Name:         httpapi.Ptr(it.NameSnapshot),
			PriceKopecks: httpapi.Ptr(it.PriceSnapshot),
			Quantity:     httpapi.Ptr(it.Quantity),
		})
	}
	status := OrderStatus(o.Status)
	return Order{
		Id:           httpapi.Ptr(o.ID),
		VenueId:      httpapi.Ptr(o.VenueID),
		Status:       &status,
		TotalKopecks: httpapi.Ptr(o.TotalKopecks),
		Currency:     httpapi.Ptr(o.Currency),
		CustomerRef:  httpapi.Ptr(o.CustomerRef),
		Items:        &items,
		CreatedAt:    httpapi.Ptr(o.CreatedAt),
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
