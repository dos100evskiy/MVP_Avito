//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/repository/postgres"
)

// TestOrderRepo_Create_AppendsStatusHistoryInSameTransaction — регрессионный
// тест на реальный баг, найденный вручную через curl (см. историю чата):
// AppendStatusHistory писала запись через пул соединений мимо ещё не
// закоммиченной транзакции создания заказа, и Postgres ронял вставку по
// внешнему ключу order_status_history_order_id_fkey, потому что с точки
// зрения отдельного соединения из пула заказ ещё не существовал.
//
// Юнит-тесты usecase на моках (internal/usecase/order_test.go) этот класс
// багов принципиально не ловят — моки ничего не знают про FK и про
// видимость незакоммиченных строк между разными соединениями. Только
// реальный Postgres в транзакции воспроизводит эту ошибку — этот тест
// как раз и есть тот самый недостающий уровень проверки.
func TestOrderRepo_Create_AppendsStatusHistoryInSameTransaction(t *testing.T) {
	ctx := context.Background()

	venueID := insertVenue(t, ctx, "Кухня для заказов", "Москва", uniqueAPIKeyHash())
	menuRepo := postgres.NewMenuRepo(testPool)
	orderRepo := postgres.NewOrderRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	itemID, err := menuRepo.CreateItem(ctx, domain.MenuItem{
		VenueID: venueID, Name: "Борщ", PriceKopecks: 39000, Currency: "RUB",
	})
	if err != nil {
		t.Fatalf("setup: create menu item: %v", err)
	}

	orderID := uuid.NewString()
	order := domain.Order{
		ID:           orderID,
		VenueID:      venueID,
		Status:       domain.OrderStatusCreated,
		TotalKopecks: 39000,
		Currency:     "RUB",
		CustomerRef:  "integration-test",
		Items: []domain.OrderItem{
			{ID: uuid.NewString(), MenuItemID: itemID, NameSnapshot: "Борщ", PriceSnapshot: 39000, Quantity: 1},
		},
	}

	tx, err := txManager.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	// Rollback после успешного Commit — no-op у pgx, безопасно оставлять defer.
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := orderRepo.Create(ctx, tx, order); err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Ключевая проверка: AppendStatusHistory ДОЛЖНА принимать ту же tx и писать
	// в её рамках — если передать nil (как было в баге), вставка упадёт по FK,
	// потому что заказ выше ещё не закоммичен и не виден другому соединению.
	if err := orderRepo.AppendStatusHistory(ctx, tx, orderID, "", domain.OrderStatusCreated, "CLIENT"); err != nil {
		t.Fatalf("append status history within tx: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// После коммита заказ и его позиции должны быть видны обычным чтением через пул.
	got, err := orderRepo.GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("get order after commit: %v", err)
	}
	if got.Status != domain.OrderStatusCreated {
		t.Errorf("expected status CREATED, got %s", got.Status)
	}
	if len(got.Items) != 1 || got.Items[0].NameSnapshot != "Борщ" {
		t.Errorf("unexpected items: %+v", got.Items)
	}

	// И сама история статуса должна быть закоммичена — читаем таблицу напрямую,
	// т.к. в domain.OrderRepository нет метода ListStatusHistory.
	var historyCount int
	err = testPool.QueryRow(ctx,
		`SELECT count(*) FROM order_status_history WHERE order_id = $1 AND to_status = 'CREATED'`,
		orderID,
	).Scan(&historyCount)
	if err != nil {
		t.Fatalf("query status history: %v", err)
	}
	if historyCount != 1 {
		t.Errorf("expected exactly 1 status history row, got %d", historyCount)
	}
}

// TestOrderRepo_AppendStatusHistory_FailsOnUncommittedOrder — "негативный
// зеркальный" тест к предыдущему: явно воспроизводит сам баг (запись истории
// МИМО открытой транзакции, до коммита заказа) и проверяет, что ограничение
// внешнего ключа действительно срабатывает. Это подтверждает, что constraint
// в схеме не бутафорский и предыдущий тест проверяет реальную защиту, а не
// случайно проходит по другой причине.
func TestOrderRepo_AppendStatusHistory_FailsOnUncommittedOrder(t *testing.T) {
	ctx := context.Background()

	venueID := insertVenue(t, ctx, "Кухня для негативного теста", "Москва", uniqueAPIKeyHash())
	orderRepo := postgres.NewOrderRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	orderID := uuid.NewString()
	order := domain.Order{
		ID: orderID, VenueID: venueID, Status: domain.OrderStatusCreated,
		TotalKopecks: 1000, Currency: "RUB", CustomerRef: "neg-test",
	}

	tx, err := txManager.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := orderRepo.Create(ctx, tx, order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	// Заказ создан, но НЕ закоммичен. Пишем историю мимо транзакции (tx=nil) —
	// это и есть воспроизведение исходного бага.
	err = orderRepo.AppendStatusHistory(ctx, nil, orderID, "", domain.OrderStatusCreated, "CLIENT")
	if err == nil {
		t.Fatal("expected FK violation error when appending history outside the open transaction, got nil")
	}
	// Не проверяем конкретный текст ошибки Postgres (он зависит от драйвера/локали),
	// достаточно факта, что операция не прошла — именно так и должно быть.
}

func TestOrderRepo_UpdateStatus_AtomicTransition(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня для статусов", "Санкт-Петербург", uniqueAPIKeyHash())
	orderRepo := postgres.NewOrderRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	orderID := createCommittedOrder(t, ctx, orderRepo, txManager, venueID)

	// Валидный переход CREATED -> ACCEPTED должен пройти.
	if err := orderRepo.UpdateStatus(ctx, orderID, domain.OrderStatusCreated, domain.OrderStatusAccepted); err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}
	got, err := orderRepo.GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if got.Status != domain.OrderStatusAccepted {
		t.Errorf("expected ACCEPTED, got %s", got.Status)
	}

	// Повторная попытка перехода из уже неактуального "from" (CREATED, а
	// заказ уже ACCEPTED) обязана провалиться по условию WHERE status = $2 —
	// это защита от гонки на уровне SQL, а не только в Go (см. usecase.ChangeStatus).
	err = orderRepo.UpdateStatus(ctx, orderID, domain.OrderStatusCreated, domain.OrderStatusCooking)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for stale from-status, got %v", err)
	}
}

func TestOrderRepo_ListByVenue_FiltersByStatus(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня для листинга заказов", "Новосибирск", uniqueAPIKeyHash())
	orderRepo := postgres.NewOrderRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	orderID1 := createCommittedOrder(t, ctx, orderRepo, txManager, venueID)
	orderID2 := createCommittedOrder(t, ctx, orderRepo, txManager, venueID)

	if err := orderRepo.UpdateStatus(ctx, orderID1, domain.OrderStatusCreated, domain.OrderStatusAccepted); err != nil {
		t.Fatalf("setup: %v", err)
	}

	created, err := orderRepo.ListByVenue(ctx, venueID, domain.OrderStatusCreated, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 1 || created[0].ID != orderID2 {
		t.Fatalf("expected only order %s with status CREATED, got %+v", orderID2, created)
	}

	all, err := orderRepo.ListByVenue(ctx, venueID, "", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 orders total for venue, got %d", len(all))
	}
}

// createCommittedOrder — общий хелпер для тестов, которым нужен уже
// существующий закоммиченный заказ в статусе CREATED, без позиций (для
// проверок статусов/листинга позиции заказа не важны).
func createCommittedOrder(t *testing.T, ctx context.Context, orderRepo *postgres.OrderRepo, txManager *postgres.TxManager, venueID string) string {
	t.Helper()

	orderID := uuid.NewString()
	order := domain.Order{
		ID: orderID, VenueID: venueID, Status: domain.OrderStatusCreated,
		TotalKopecks: 1000, Currency: "RUB", CustomerRef: "fixture",
	}

	tx, err := txManager.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := orderRepo.Create(ctx, tx, order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := orderRepo.AppendStatusHistory(ctx, tx, orderID, "", domain.OrderStatusCreated, "CLIENT"); err != nil {
		t.Fatalf("append status history: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return orderID
}
