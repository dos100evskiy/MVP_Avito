package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/transport/http/client"
	"github.com/avito/kuhnya/core/internal/usecase"
)

// --- моки CatalogUseCase/OrderUseCase (интерфейсы объявлены в client-пакете
// специально ради этого — см. комментарий в handlers.go) ---

type catalogMock struct {
	listVenues   func(ctx context.Context, city string, limit, offset int) ([]domain.Venue, error)
	getVenueMenu func(ctx context.Context, venueID string) (*usecase.VenueMenu, error)
}

func (m *catalogMock) ListVenues(ctx context.Context, city string, limit, offset int) ([]domain.Venue, error) {
	return m.listVenues(ctx, city, limit, offset)
}

func (m *catalogMock) GetVenueMenu(ctx context.Context, venueID string) (*usecase.VenueMenu, error) {
	return m.getVenueMenu(ctx, venueID)
}

type orderMock struct {
	createOrder func(ctx context.Context, venueID, customerRef string, items []domain.NewOrderItemInput) (*domain.Order, error)
	getOrder    func(ctx context.Context, id string) (*domain.Order, error)
	cancelOrder func(ctx context.Context, orderID string) error
}

func (m *orderMock) CreateOrder(ctx context.Context, venueID, customerRef string, items []domain.NewOrderItemInput) (*domain.Order, error) {
	return m.createOrder(ctx, venueID, customerRef, items)
}

func (m *orderMock) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return m.getOrder(ctx, id)
}

func (m *orderMock) CancelOrder(ctx context.Context, orderID string) error {
	return m.cancelOrder(ctx, orderID)
}

// newTestRouter собирает изолированный роутер только с Client API — без
// server.NewRouter (не нужны middleware/VenueRepo), настоящий httptest,
// настоящий chi, но usecase-слой замокан, БД нет вообще.
func newTestRouter(catalog client.CatalogUseCase, orders client.OrderUseCase) http.Handler {
	r := chi.NewRouter()
	client.New(catalog, orders).Mount(r)
	return r
}

func doRequest(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return v
}

// --- listVenues ---

func TestListVenues_Success(t *testing.T) {
	var gotCity string
	catalog := &catalogMock{
		listVenues: func(_ context.Context, city string, _, _ int) ([]domain.Venue, error) {
			gotCity = city
			return []domain.Venue{
				{ID: "venue-1", Name: "Тестовая кухня", City: "Москва"},
			}, nil
		},
	}
	h := newTestRouter(catalog, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/venues?city=Москва", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotCity != "Москва" {
		t.Errorf("expected city filter forwarded as 'Москва', got %q", gotCity)
	}
	venues := decodeJSON[[]client.Venue](t, rec)
	if len(venues) != 1 || venues[0].Id == nil || *venues[0].Id != "venue-1" {
		t.Errorf("unexpected venues in response: %+v", venues)
	}
	if venues[0].Name == nil || *venues[0].Name != "Тестовая кухня" {
		t.Errorf("unexpected venue name: %+v", venues[0])
	}
}

func TestListVenues_UsecaseError(t *testing.T) {
	catalog := &catalogMock{
		listVenues: func(context.Context, string, int, int) ([]domain.Venue, error) {
			return nil, domain.ErrVenueNotFound // произвольная доменная ошибка для проверки маппинга
		},
	}
	h := newTestRouter(catalog, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/venues", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- getVenueMenu ---

func TestGetVenueMenu_Success(t *testing.T) {
	catalog := &catalogMock{
		getVenueMenu: func(_ context.Context, venueID string) (*usecase.VenueMenu, error) {
			if venueID != "venue-1" {
				t.Errorf("unexpected venueID: %s", venueID)
			}
			return &usecase.VenueMenu{
				Venue:      domain.Venue{ID: "venue-1", Name: "Тестовая кухня", City: "Москва"},
				Categories: []domain.MenuCategory{{ID: "cat-1", Name: "Супы"}},
				Items: []domain.MenuItem{
					{ID: "item-1", Name: "Борщ", PriceKopecks: 39000, Currency: "RUB", IsAvailable: true},
				},
			}, nil
		},
	}
	h := newTestRouter(catalog, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/venues/venue-1/menu", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	menu := decodeJSON[client.VenueMenu](t, rec)
	if menu.Venue == nil || menu.Venue.Id == nil || *menu.Venue.Id != "venue-1" {
		t.Fatalf("unexpected venue in response: %+v", menu.Venue)
	}
	if menu.Categories == nil || len(*menu.Categories) != 1 {
		t.Fatalf("expected 1 category, got %+v", menu.Categories)
	}
	if menu.Items == nil || len(*menu.Items) != 1 || *(*menu.Items)[0].PriceKopecks != 39000 {
		t.Fatalf("unexpected items in response: %+v", menu.Items)
	}
}

func TestGetVenueMenu_NotFound(t *testing.T) {
	catalog := &catalogMock{
		getVenueMenu: func(context.Context, string) (*usecase.VenueMenu, error) {
			return nil, domain.ErrVenueNotFound
		},
	}
	h := newTestRouter(catalog, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/venues/unknown/menu", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- createOrder ---

func TestCreateOrder_Success(t *testing.T) {
	var gotVenueID, gotCustomerRef string
	var gotItems []domain.NewOrderItemInput

	orders := &orderMock{
		createOrder: func(_ context.Context, venueID, customerRef string, items []domain.NewOrderItemInput) (*domain.Order, error) {
			gotVenueID, gotCustomerRef, gotItems = venueID, customerRef, items
			return &domain.Order{
				ID: "order-1", VenueID: venueID, Status: domain.OrderStatusCreated,
				TotalKopecks: 78000, Currency: "RUB", CustomerRef: customerRef,
				CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	body := client.CreateOrderRequest{
		VenueId:     "venue-1",
		CustomerRef: "customer-42",
		Items:       []client.CreateOrderItem{{MenuItemId: "item-1", Quantity: 2}},
	}
	rec := doRequest(t, h, http.MethodPost, "/orders", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotVenueID != "venue-1" || gotCustomerRef != "customer-42" {
		t.Errorf("unexpected forwarded args: venueID=%s customerRef=%s", gotVenueID, gotCustomerRef)
	}
	if len(gotItems) != 1 || gotItems[0].MenuItemID != "item-1" || gotItems[0].Quantity != 2 {
		t.Errorf("unexpected forwarded items: %+v", gotItems)
	}

	order := decodeJSON[client.Order](t, rec)
	if order.Id == nil || *order.Id != "order-1" {
		t.Errorf("unexpected order id in response: %+v", order.Id)
	}
	if order.Status == nil || *order.Status != client.CREATED {
		t.Errorf("expected status CREATED, got %+v", order.Status)
	}
}

func TestCreateOrder_InvalidJSON(t *testing.T) {
	h := newTestRouter(&catalogMock{}, &orderMock{})

	req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader([]byte("not-json")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrder_MissingVenueID(t *testing.T) {
	h := newTestRouter(&catalogMock{}, &orderMock{})

	body := client.CreateOrderRequest{
		CustomerRef: "customer-42",
		Items:       []client.CreateOrderItem{{MenuItemId: "item-1", Quantity: 1}},
	}
	rec := doRequest(t, h, http.MethodPost, "/orders", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (venueId required), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrder_ItemsUnavailable(t *testing.T) {
	orders := &orderMock{
		createOrder: func(context.Context, string, string, []domain.NewOrderItemInput) (*domain.Order, error) {
			return nil, &domain.ErrItemsUnavailable{Items: []domain.UnavailableItem{
				{MenuItemID: "item-2", Reason: "unavailable"},
			}}
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	body := client.CreateOrderRequest{
		VenueId:     "venue-1",
		CustomerRef: "customer-42",
		Items:       []client.CreateOrderItem{{MenuItemId: "item-2", Quantity: 1}},
	}
	rec := doRequest(t, h, http.MethodPost, "/orders", body)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	unavail := decodeJSON[client.ItemsUnavailableError](t, rec)
	if unavail.Details == nil || len(*unavail.Details) != 1 {
		t.Fatalf("expected 1 unavailable item detail, got %+v", unavail.Details)
	}
	if *(*unavail.Details)[0].MenuItemId != "item-2" {
		t.Errorf("unexpected unavailable item id: %+v", (*unavail.Details)[0])
	}
}

// --- getOrder / cancelOrder ---

func TestGetOrder_Success(t *testing.T) {
	orders := &orderMock{
		getOrder: func(_ context.Context, id string) (*domain.Order, error) {
			if id != "order-1" {
				t.Errorf("unexpected order id: %s", id)
			}
			return &domain.Order{ID: id, Status: domain.OrderStatusCooking}, nil
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	rec := doRequest(t, h, http.MethodGet, "/orders/order-1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	order := decodeJSON[client.Order](t, rec)
	if order.Status == nil || *order.Status != client.COOKING {
		t.Errorf("expected status COOKING, got %+v", order.Status)
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	orders := &orderMock{
		getOrder: func(context.Context, string) (*domain.Order, error) {
			return nil, domain.ErrOrderNotFound
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	rec := doRequest(t, h, http.MethodGet, "/orders/unknown", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelOrder_Success(t *testing.T) {
	var gotOrderID string
	orders := &orderMock{
		cancelOrder: func(_ context.Context, orderID string) error {
			gotOrderID = orderID
			return nil
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	rec := doRequest(t, h, http.MethodPost, "/orders/order-1/cancel", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotOrderID != "order-1" {
		t.Errorf("unexpected order id forwarded: %s", gotOrderID)
	}
}

func TestCancelOrder_InvalidTransition(t *testing.T) {
	orders := &orderMock{
		cancelOrder: func(context.Context, string) error {
			return domain.ErrInvalidTransition
		},
	}
	h := newTestRouter(&catalogMock{}, orders)

	rec := doRequest(t, h, http.MethodPost, "/orders/order-1/cancel", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
