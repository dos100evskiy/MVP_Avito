package usecase_test

import (
	"context"
	"sync"

	"github.com/avito/kuhnya/core/internal/domain"
)

// Простые ручные моки без mockgen/counterfeiter — для MVP-скелета достаточно
// и они не требуют кодогенерации. При росте проекта естественная замена —
// mockgen поверх domain-интерфейсов.

type venueRepoMock struct {
	byID map[string]domain.Venue
}

func (m *venueRepoMock) GetByID(_ context.Context, id string) (*domain.Venue, error) {
	v, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrVenueNotFound
	}
	return &v, nil
}
func (m *venueRepoMock) GetByAPIKeyHash(_ context.Context, hash string) (*domain.Venue, error) {
	for _, v := range m.byID {
		if v.APIKeyHash == hash {
			return &v, nil
		}
	}
	return nil, domain.ErrVenueNotFound
}
func (m *venueRepoMock) List(_ context.Context, city string, limit, offset int) ([]domain.Venue, error) {
	var out []domain.Venue
	for _, v := range m.byID {
		if city == "" || v.City == city {
			out = append(out, v)
		}
	}
	return out, nil
}

type menuRepoMock struct {
	items map[string]domain.MenuItem
}

func (m *menuRepoMock) ListCategories(_ context.Context, _ string) ([]domain.MenuCategory, error) {
	return nil, nil
}
func (m *menuRepoMock) CreateCategory(_ context.Context, c domain.MenuCategory) (string, error) {
	return "cat-1", nil
}
func (m *menuRepoMock) ListItems(_ context.Context, venueID string) ([]domain.MenuItem, error) {
	var out []domain.MenuItem
	for _, it := range m.items {
		if it.VenueID == venueID {
			out = append(out, it)
		}
	}
	return out, nil
}
func (m *menuRepoMock) GetItem(_ context.Context, id string) (*domain.MenuItem, error) {
	it, ok := m.items[id]
	if !ok {
		return nil, domain.ErrMenuItemNotFound
	}
	return &it, nil
}
func (m *menuRepoMock) GetItemsForUpdate(_ context.Context, _ domain.Tx, ids []string) ([]domain.MenuItem, error) {
	var out []domain.MenuItem
	for _, id := range ids {
		if it, ok := m.items[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}
func (m *menuRepoMock) CreateItem(_ context.Context, item domain.MenuItem) (string, error) {
	return "item-new", nil
}
func (m *menuRepoMock) UpdateItem(_ context.Context, item domain.MenuItem) error { return nil }
func (m *menuRepoMock) SetAvailability(_ context.Context, id string, available bool) error {
	it, ok := m.items[id]
	if !ok {
		return domain.ErrMenuItemNotFound
	}
	it.IsAvailable = available
	m.items[id] = it
	return nil
}

type orderRepoMock struct {
	mu           sync.Mutex
	orders       map[string]domain.Order
	history      []string
	lastTxNotNil bool // фиксирует, была ли передана открытая tx в последний вызов AppendStatusHistory
}

func newOrderRepoMock() *orderRepoMock {
	return &orderRepoMock{orders: make(map[string]domain.Order)}
}

func (m *orderRepoMock) Create(_ context.Context, _ domain.Tx, order domain.Order) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders[order.ID] = order
	return order.ID, nil
}
func (m *orderRepoMock) GetByID(_ context.Context, id string) (*domain.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	return &o, nil
}
func (m *orderRepoMock) ListByVenue(_ context.Context, venueID string, status domain.OrderStatus, _, _ int) ([]domain.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Order
	for _, o := range m.orders {
		if o.VenueID == venueID && (status == "" || o.Status == status) {
			out = append(out, o)
		}
	}
	return out, nil
}
func (m *orderRepoMock) UpdateStatus(_ context.Context, id string, from, to domain.OrderStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if o.Status != from {
		return domain.ErrInvalidTransition
	}
	o.Status = to
	m.orders[id] = o
	return nil
}
func (m *orderRepoMock) AppendStatusHistory(_ context.Context, tx domain.Tx, orderID string, from, to domain.OrderStatus, changedBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastTxNotNil = tx != nil
	m.history = append(m.history, orderID+":"+string(from)+"->"+string(to)+"("+changedBy+")")
	return nil
}

type noopTxManager struct{}
type noopTx struct{}

func (noopTx) Commit(_ context.Context) error   { return nil }
func (noopTx) Rollback(_ context.Context) error { return nil }

func (noopTxManager) Begin(_ context.Context) (domain.Tx, error) { return noopTx{}, nil }

type noopNotifier struct{ calls int }

func (n *noopNotifier) NotifyNewOrder(_ context.Context, _ domain.Venue, _ domain.Order) {
	n.calls++
}
