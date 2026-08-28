package venue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/transport/http/middleware"
	"github.com/avito/kuhnya/core/internal/transport/http/venue"
)

// --- моки MenuUseCase/OrderUseCase (интерфейсы объявлены в venue-пакете) ---

type menuMock struct {
	createItem      func(ctx context.Context, item domain.MenuItem) (string, error)
	setAvailability func(ctx context.Context, itemID string, available bool) error
}

func (m *menuMock) CreateItem(ctx context.Context, item domain.MenuItem) (string, error) {
	return m.createItem(ctx, item)
}

func (m *menuMock) SetAvailability(ctx context.Context, itemID string, available bool) error {
	return m.setAvailability(ctx, itemID, available)
}

type orderMock struct {
	listVenueOrders func(ctx context.Context, venueID string, status domain.OrderStatus, limit, offset int) ([]domain.Order, error)
	changeStatus    func(ctx context.Context, orderID string, to domain.OrderStatus, changedBy string) error
}

func (m *orderMock) ListVenueOrders(ctx context.Context, venueID string, status domain.OrderStatus, limit, offset int) ([]domain.Order, error) {
	return m.listVenueOrders(ctx, venueID, status, limit, offset)
}

func (m *orderMock) ChangeStatus(ctx context.Context, orderID string, to domain.OrderStatus, changedBy string) error {
	return m.changeStatus(ctx, orderID, to, changedBy)
}

// --- мок domain.VenueRepository — только для middleware.VenueAuth ---

const testAPIKey = "test-venue-key"
const testVenueID = "venue-1"

type venueRepoMock struct {
	// active управляет тем, найдётся ли заведение по любому переданному
	// X-API-Key (мы не пересчитываем sha256 в тесте вручную — просто
	// эмулируем "ключ валиден"/"ключ невалиден" через этот флаг).
	active bool
}

func (m *venueRepoMock) GetByID(context.Context, string) (*domain.Venue, error) {
	return nil, domain.ErrVenueNotFound
}

func (m *venueRepoMock) GetByAPIKeyHash(context.Context, string) (*domain.Venue, error) {
	if !m.active {
		return nil, domain.ErrVenueNotFound
	}
	return &domain.Venue{ID: testVenueID, IsActive: true}, nil
}

func (m *venueRepoMock) List(context.Context, string, int, int) ([]domain.Venue, error) {
	return nil, nil
}

// newTestRouter собирает роутер с НАСТОЯЩИМ middleware.VenueAuth поверх
// замоканных usecase — так проверяется весь реальный путь запроса, включая
// аутентификацию по X-API-Key, а не только бизнес-логика хендлеров.
func newTestRouter(venues domain.VenueRepository, menu venue.MenuUseCase, orders venue.OrderUseCase) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.VenueAuth(venues))
	venue.New(menu, orders).Mount(r)
	return r
}

func doRequest(t *testing.T, h http.Handler, method, path, apiKey string, body interface{}) *httptest.ResponseRecorder {
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
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
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

// --- авторизация (общая для всех роутов пакета — проверяем на одном эндпоинте) ---

func TestAuth_MissingAPIKey(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/orders", "", nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without X-API-Key, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuth_InvalidAPIKey(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: false}, &menuMock{}, &orderMock{})

	rec := doRequest(t, h, http.MethodGet, "/orders", testAPIKey, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with unknown X-API-Key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- createMenuItem ---

func TestCreateMenuItem_Success(t *testing.T) {
	var got domain.MenuItem
	menu := &menuMock{
		createItem: func(_ context.Context, item domain.MenuItem) (string, error) {
			got = item
			return "item-new", nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, menu, &orderMock{})

	body := venue.CreateMenuItemRequest{Name: "Плов", PriceKopecks: 55000}
	rec := doRequest(t, h, http.MethodPost, "/menu-items", testAPIKey, body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got.VenueID != testVenueID {
		t.Errorf("expected venueID from auth context (%s), got %q", testVenueID, got.VenueID)
	}
	if got.Name != "Плов" || got.PriceKopecks != 55000 {
		t.Errorf("unexpected forwarded item: %+v", got)
	}
	if got.Currency != "RUB" {
		t.Errorf("expected default currency RUB when omitted, got %q", got.Currency)
	}

	resp := decodeJSON[map[string]string](t, rec)
	if resp["id"] != "item-new" {
		t.Errorf("unexpected response body: %+v", resp)
	}
}

func TestCreateMenuItem_InvalidJSON(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, &orderMock{})

	req := httptest.NewRequest(http.MethodPost, "/menu-items", bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-API-Key", testAPIKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- setAvailability ---

func TestSetAvailability_Success(t *testing.T) {
	var gotID string
	var gotAvailable bool
	menu := &menuMock{
		setAvailability: func(_ context.Context, itemID string, available bool) error {
			gotID, gotAvailable = itemID, available
			return nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, menu, &orderMock{})

	body := venue.AvailabilityRequest{IsAvailable: boolPtr(false)}
	rec := doRequest(t, h, http.MethodPatch, "/menu-items/item-1/availability", testAPIKey, body)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotID != "item-1" || gotAvailable != false {
		t.Errorf("unexpected forwarded args: id=%s available=%v", gotID, gotAvailable)
	}
}

func TestSetAvailability_NotFound(t *testing.T) {
	menu := &menuMock{
		setAvailability: func(context.Context, string, bool) error {
			return domain.ErrMenuItemNotFound
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, menu, &orderMock{})

	body := venue.AvailabilityRequest{IsAvailable: boolPtr(true)}
	rec := doRequest(t, h, http.MethodPatch, "/menu-items/unknown/availability", testAPIKey, body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetAvailability_InvalidJSON(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, &orderMock{})

	req := httptest.NewRequest(http.MethodPatch, "/menu-items/item-1/availability", bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-API-Key", testAPIKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- listOrders ---

func TestListOrders_Success(t *testing.T) {
	var gotVenueID string
	var gotStatus domain.OrderStatus
	orders := &orderMock{
		listVenueOrders: func(_ context.Context, venueID string, status domain.OrderStatus, _, _ int) ([]domain.Order, error) {
			gotVenueID, gotStatus = venueID, status
			return []domain.Order{{ID: "order-1", VenueID: venueID, Status: domain.OrderStatusCreated}}, nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	rec := doRequest(t, h, http.MethodGet, "/orders?status=CREATED", testAPIKey, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotVenueID != testVenueID {
		t.Errorf("expected venueID from auth context, got %q", gotVenueID)
	}
	if gotStatus != domain.OrderStatusCreated {
		t.Errorf("expected status filter CREATED forwarded, got %q", gotStatus)
	}

	list := decodeJSON[[]venue.Order](t, rec)
	if len(list) != 1 || list[0].Id == nil || *list[0].Id != "order-1" {
		t.Fatalf("unexpected orders in response: %+v", list)
	}
}

func TestListOrders_UsecaseError(t *testing.T) {
	orders := &orderMock{
		listVenueOrders: func(context.Context, string, domain.OrderStatus, int, int) ([]domain.Order, error) {
			return nil, domain.ErrOrderNotFound
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	rec := doRequest(t, h, http.MethodGet, "/orders", testAPIKey, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- accept / reject / status ---

func TestAcceptOrder_Success(t *testing.T) {
	var gotOrderID string
	var gotTo domain.OrderStatus
	var gotBy string
	orders := &orderMock{
		changeStatus: func(_ context.Context, orderID string, to domain.OrderStatus, changedBy string) error {
			gotOrderID, gotTo, gotBy = orderID, to, changedBy
			return nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	rec := doRequest(t, h, http.MethodPost, "/orders/order-1/accept", testAPIKey, nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotOrderID != "order-1" || gotTo != domain.OrderStatusAccepted || gotBy != "VENUE" {
		t.Errorf("unexpected ChangeStatus call: orderID=%s to=%s by=%s", gotOrderID, gotTo, gotBy)
	}
}

func TestRejectOrder_Success(t *testing.T) {
	var gotTo domain.OrderStatus
	orders := &orderMock{
		changeStatus: func(_ context.Context, _ string, to domain.OrderStatus, _ string) error {
			gotTo = to
			return nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	body := venue.RejectOrderRequest{Reason: strPtr("нет в наличии")}
	rec := doRequest(t, h, http.MethodPost, "/orders/order-1/reject", testAPIKey, body)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotTo != domain.OrderStatusCancelled {
		t.Errorf("expected transition to CANCELLED, got %q", gotTo)
	}
}

func TestUpdateOrderStatus_Success(t *testing.T) {
	var gotTo domain.OrderStatus
	orders := &orderMock{
		changeStatus: func(_ context.Context, _ string, to domain.OrderStatus, _ string) error {
			gotTo = to
			return nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	status := venue.UpdateOrderStatusRequestStatusCOOKING
	body := venue.UpdateOrderStatusRequest{Status: &status}
	rec := doRequest(t, h, http.MethodPatch, "/orders/order-1/status", testAPIKey, body)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotTo != domain.OrderStatusCooking {
		t.Errorf("expected transition to COOKING, got %q", gotTo)
	}
}

func TestUpdateOrderStatus_MissingStatus(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, &orderMock{})

	rec := doRequest(t, h, http.MethodPatch, "/orders/order-1/status", testAPIKey, venue.UpdateOrderStatusRequest{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when status is omitted, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateOrderStatus_InvalidJSON(t *testing.T) {
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, &orderMock{})

	req := httptest.NewRequest(http.MethodPatch, "/orders/order-1/status", bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-API-Key", testAPIKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangeStatus_InvalidTransition(t *testing.T) {
	orders := &orderMock{
		changeStatus: func(context.Context, string, domain.OrderStatus, string) error {
			return domain.ErrInvalidTransition
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, &menuMock{}, orders)

	rec := doRequest(t, h, http.MethodPost, "/orders/order-1/accept", testAPIKey, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateMenuItem_WithOptionalFields(t *testing.T) {
	var got domain.MenuItem
	menu := &menuMock{
		createItem: func(_ context.Context, item domain.MenuItem) (string, error) {
			got = item
			return "item-new", nil
		},
	}
	h := newTestRouter(&venueRepoMock{active: true}, menu, &orderMock{})

	categoryID := "cat-1"
	body := venue.CreateMenuItemRequest{
		Name: "Стейк", PriceKopecks: 89000,
		CategoryId:  &categoryID,
		Description: strPtr("с кровью"),
		Currency:    strPtr("USD"),
	}
	rec := doRequest(t, h, http.MethodPost, "/menu-items", testAPIKey, body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got.CategoryID == nil || *got.CategoryID != "cat-1" {
		t.Errorf("expected categoryId forwarded, got %+v", got.CategoryID)
	}
	if got.Description != "с кровью" {
		t.Errorf("expected description forwarded, got %q", got.Description)
	}
	if got.Currency != "USD" {
		t.Errorf("expected explicit currency to override default, got %q", got.Currency)
	}
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }
