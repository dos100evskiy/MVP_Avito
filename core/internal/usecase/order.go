package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/avito/kuhnya/core/internal/domain"
)

// OrderNotifier — порт для нотификации заведения о новом заказе (webhook).
// Реализация (HTTP POST на venue.CallbackURL) живёт в infrastructure-слое.
// Если callback недоступен/не задан — заведение просто узнает о заказе через
// поллинг Venue API (GET /orders?status=CREATED), это осознанный fallback.
type OrderNotifier interface {
	NotifyNewOrder(ctx context.Context, venue domain.Venue, order domain.Order)
}

type OrderUseCase struct {
	venues   domain.VenueRepository
	menu     domain.MenuRepository
	orders   domain.OrderRepository
	tx       domain.TxManager
	notifier OrderNotifier
}

func NewOrderUseCase(v domain.VenueRepository, m domain.MenuRepository, o domain.OrderRepository, tx domain.TxManager, n OrderNotifier) *OrderUseCase {
	return &OrderUseCase{venues: v, menu: m, orders: o, tx: tx, notifier: n}
}

// CreateOrder реализует сценарий "клиент оформляет заказ" из CJM:
//  1. все позиции должны принадлежать одному заведению;
//  2. цена и наличие берутся из БД под блокировкой строк (SELECT ... FOR UPDATE),
//     а не из запроса клиента — иначе фронт мог бы подделать цену или заказать
//     то, что закончилось между просмотром меню и оформлением заказа;
//  3. если что-то недоступно — возвращаем составную ошибку со списком проблемных
//     позиций, а не просто общий 409.
func (uc *OrderUseCase) CreateOrder(ctx context.Context, venueID, customerRef string, itemsInput []domain.NewOrderItemInput) (*domain.Order, error) {
	if len(itemsInput) == 0 {
		return nil, domain.ErrEmptyOrder
	}

	venue, err := uc.venues.GetByID(ctx, venueID)
	if err != nil {
		return nil, err
	}

	tx, err := uc.tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback после commit — no-op у большинства драйверов

	ids := make([]string, 0, len(itemsInput))
	for _, in := range itemsInput {
		ids = append(ids, in.MenuItemID)
	}

	items, err := uc.menu.GetItemsForUpdate(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	itemsByID := make(map[string]domain.MenuItem, len(items))
	for _, it := range items {
		itemsByID[it.ID] = it
	}

	var unavailable []domain.UnavailableItem
	orderItems := make([]domain.OrderItem, 0, len(itemsInput))
	var total int64

	for _, in := range itemsInput {
		item, ok := itemsByID[in.MenuItemID]
		if !ok {
			unavailable = append(unavailable, domain.UnavailableItem{MenuItemID: in.MenuItemID, Reason: "not found"})
			continue
		}
		if item.VenueID != venueID {
			return nil, domain.ErrItemsFromDiffVenues
		}
		if !item.IsAvailable {
			unavailable = append(unavailable, domain.UnavailableItem{MenuItemID: in.MenuItemID, Reason: "unavailable"})
			continue
		}
		if in.Quantity <= 0 {
			unavailable = append(unavailable, domain.UnavailableItem{MenuItemID: in.MenuItemID, Reason: "invalid quantity"})
			continue
		}

		orderItems = append(orderItems, domain.OrderItem{
			ID:            uuid.NewString(),
			MenuItemID:    item.ID,
			NameSnapshot:  item.Name,
			PriceSnapshot: item.PriceKopecks,
			Quantity:      in.Quantity,
		})
		total += item.PriceKopecks * int64(in.Quantity)
	}

	if len(unavailable) > 0 {
		return nil, &domain.ErrItemsUnavailable{Items: unavailable}
	}

	order := domain.Order{
		ID:           uuid.NewString(),
		VenueID:      venueID,
		Status:       domain.OrderStatusCreated,
		TotalKopecks: total,
		Currency:     "RUB",
		CustomerRef:  customerRef,
		Items:        orderItems,
	}

	if _, err := uc.orders.Create(ctx, tx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if err := uc.orders.AppendStatusHistory(ctx, tx, order.ID, "", domain.OrderStatusCreated, "CLIENT"); err != nil {
		return nil, fmt.Errorf("append history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	if uc.notifier != nil {
		// Нотификация — best-effort и не должна ломать основной сценарий,
		// поэтому вызывается уже после успешного коммита и не влияет на ответ клиенту.
		uc.notifier.NotifyNewOrder(ctx, *venue, order)
	}

	return &order, nil
}

func (uc *OrderUseCase) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return uc.orders.GetByID(ctx, id)
}

// ChangeStatus — единая точка смены статуса заказа, используется и клиентским
// сценарием "отмена", и venue-сценариями "принять/отклонить/приготовить".
// changedBy ∈ {"CLIENT","VENUE","SYSTEM"} — попадает в order_status_history.
func (uc *OrderUseCase) ChangeStatus(ctx context.Context, orderID string, to domain.OrderStatus, changedBy string) error {
	order, err := uc.orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if !domain.CanTransition(order.Status, to) {
		return domain.ErrInvalidTransition
	}
	if err := uc.orders.UpdateStatus(ctx, orderID, order.Status, to); err != nil {
		return err
	}
	return uc.orders.AppendStatusHistory(ctx, nil, orderID, order.Status, to, changedBy)
}

func (uc *OrderUseCase) CancelOrder(ctx context.Context, orderID string) error {
	return uc.ChangeStatus(ctx, orderID, domain.OrderStatusCancelled, "CLIENT")
}

func (uc *OrderUseCase) ListVenueOrders(ctx context.Context, venueID string, status domain.OrderStatus, limit, offset int) ([]domain.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return uc.orders.ListByVenue(ctx, venueID, status, limit, offset)
}
