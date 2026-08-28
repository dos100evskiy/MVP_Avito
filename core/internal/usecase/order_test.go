package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/usecase"
)

func newOrderUseCase(t *testing.T) (*usecase.OrderUseCase, *venueRepoMock, *menuRepoMock, *orderRepoMock, *noopNotifier) {
	t.Helper()

	venues := &venueRepoMock{byID: map[string]domain.Venue{
		"venue-1": {ID: "venue-1", Name: "Тестовая кухня", City: "Москва", IsActive: true},
	}}
	menu := &menuRepoMock{items: map[string]domain.MenuItem{
		"item-1": {ID: "item-1", VenueID: "venue-1", Name: "Борщ", PriceKopecks: 39000, IsAvailable: true},
		"item-2": {ID: "item-2", VenueID: "venue-1", Name: "Стейк", PriceKopecks: 89000, IsAvailable: false},
		"item-3": {ID: "item-3", VenueID: "venue-2", Name: "Чужое блюдо", PriceKopecks: 10000, IsAvailable: true},
	}}
	orders := newOrderRepoMock()
	notifier := &noopNotifier{}

	uc := usecase.NewOrderUseCase(venues, menu, orders, noopTxManager{}, notifier)
	return uc, venues, menu, orders, notifier
}

func TestCreateOrder_Success(t *testing.T) {
	uc, _, _, _, notifier := newOrderUseCase(t)

	order, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != domain.OrderStatusCreated {
		t.Errorf("expected status CREATED, got %s", order.Status)
	}
	if order.TotalKopecks != 78000 {
		t.Errorf("expected total 78000, got %d", order.TotalKopecks)
	}
	if notifier.calls != 1 {
		t.Errorf("expected notifier to be called once, got %d", notifier.calls)
	}
}

func TestCreateOrder_UnavailableItem(t *testing.T) {
	uc, _, _, _, _ := newOrderUseCase(t)

	_, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-2", Quantity: 1}, // is_available = false
	})

	var unavailable *domain.ErrItemsUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected ErrItemsUnavailable, got %v", err)
	}
	if len(unavailable.Items) != 1 || unavailable.Items[0].MenuItemID != "item-2" {
		t.Errorf("unexpected unavailable items: %+v", unavailable.Items)
	}
}

func TestCreateOrder_ItemsFromDifferentVenues(t *testing.T) {
	uc, _, _, _, _ := newOrderUseCase(t)

	_, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 1},
		{MenuItemID: "item-3", Quantity: 1}, // принадлежит venue-2
	})
	if !errors.Is(err, domain.ErrItemsFromDiffVenues) {
		t.Fatalf("expected ErrItemsFromDiffVenues, got %v", err)
	}
}

func TestCreateOrder_EmptyItems(t *testing.T) {
	uc, _, _, _, _ := newOrderUseCase(t)

	_, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", nil)
	if !errors.Is(err, domain.ErrEmptyOrder) {
		t.Fatalf("expected ErrEmptyOrder, got %v", err)
	}
}

func TestChangeStatus_ValidTransition(t *testing.T) {
	uc, _, _, orders, _ := newOrderUseCase(t)

	order, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := uc.ChangeStatus(context.Background(), order.ID, domain.OrderStatusAccepted, "VENUE"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := orders.GetByID(context.Background(), order.ID)
	if got.Status != domain.OrderStatusAccepted {
		t.Errorf("expected ACCEPTED, got %s", got.Status)
	}
}

func TestChangeStatus_InvalidTransition(t *testing.T) {
	uc, _, _, _, _ := newOrderUseCase(t)

	order, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// CREATED -> READY напрямую запрещён (см. domain.CanTransition)
	err = uc.ChangeStatus(context.Background(), order.ID, domain.OrderStatusReady, "VENUE")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

// TestCreateOrder_HistoryWrittenInSameTx — регрессионный тест на баг, где
// AppendStatusHistory писалась через отдельное соединение из пула (tx == nil)
// внутри ещё не закоммиченной транзакции создания заказа. На реальном
// Postgres это ловится ошибкой внешнего ключа order_status_history_order_id_fkey
// ("заказ ещё не виден снаружи транзакции"), а моки в юнит-тестах эту гонку
// сами по себе не видят — поэтому здесь дополнительно проверяем контракт:
// при создании заказа история обязана писаться той же транзакцией, что и сам заказ.
func TestCreateOrder_HistoryWrittenInSameTx(t *testing.T) {
	uc, _, _, orders, _ := newOrderUseCase(t)

	_, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	orders.mu.Lock()
	defer orders.mu.Unlock()
	if !orders.lastTxNotNil {
		t.Fatal("AppendStatusHistory при создании заказа должна получать открытую транзакцию (tx != nil), " +
			"иначе на реальном Postgres упадёт FK-constraint order_status_history_order_id_fkey")
	}
}

func TestCancelOrder(t *testing.T) {
	uc, _, _, orders, _ := newOrderUseCase(t)

	order, err := uc.CreateOrder(context.Background(), "venue-1", "customer-42", []domain.NewOrderItemInput{
		{MenuItemID: "item-1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := uc.CancelOrder(context.Background(), order.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := orders.GetByID(context.Background(), order.ID)
	if got.Status != domain.OrderStatusCancelled {
		t.Errorf("expected CANCELLED, got %s", got.Status)
	}
}
