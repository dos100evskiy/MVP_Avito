//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/repository/postgres"
)

func TestMenuRepo_CreateAndGetItem(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня для меню", "Самара", uniqueAPIKeyHash())
	repo := postgres.NewMenuRepo(testPool)

	id, err := repo.CreateItem(ctx, domain.MenuItem{
		VenueID:      venueID,
		Name:         "Плов",
		Description:  "С бараниной",
		PriceKopecks: 55000,
		Currency:     "RUB",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty generated id")
	}

	got, err := repo.GetItem(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Плов" || got.PriceKopecks != 55000 {
		t.Errorf("unexpected item: %+v", got)
	}
	if !got.IsAvailable {
		t.Error("expected new item to be available by default (CreateItem forces is_available=true)")
	}
}

func TestMenuRepo_GetItem_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewMenuRepo(testPool)

	_, err := repo.GetItem(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrMenuItemNotFound) {
		t.Fatalf("expected ErrMenuItemNotFound, got %v", err)
	}
}

func TestMenuRepo_SetAvailability(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня для availability", "Пермь", uniqueAPIKeyHash())
	repo := postgres.NewMenuRepo(testPool)

	id, err := repo.CreateItem(ctx, domain.MenuItem{VenueID: venueID, Name: "Стейк", PriceKopecks: 89000, Currency: "RUB"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := repo.SetAvailability(ctx, id, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := repo.GetItem(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IsAvailable {
		t.Error("expected item to be unavailable after SetAvailability(false)")
	}

	err = repo.SetAvailability(ctx, "00000000-0000-0000-0000-000000000000", true)
	if !errors.Is(err, domain.ErrMenuItemNotFound) {
		t.Fatalf("expected ErrMenuItemNotFound for unknown id, got %v", err)
	}
}

func TestMenuRepo_ListItems_ScopedToVenue(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewMenuRepo(testPool)

	venueA := insertVenue(t, ctx, "Кухня A", "Казань", uniqueAPIKeyHash())
	venueB := insertVenue(t, ctx, "Кухня B", "Казань", uniqueAPIKeyHash())

	if _, err := repo.CreateItem(ctx, domain.MenuItem{VenueID: venueA, Name: "Item A1", PriceKopecks: 1000, Currency: "RUB"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := repo.CreateItem(ctx, domain.MenuItem{VenueID: venueB, Name: "Item B1", PriceKopecks: 1000, Currency: "RUB"}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	items, err := repo.ListItems(ctx, venueA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Item A1" {
		t.Fatalf("expected only venue A's item, got %+v", items)
	}
}

// TestMenuRepo_GetItemsForUpdate_WithinTransaction проверяет тот самый путь,
// которым идёт usecase.OrderUseCase.CreateOrder: SELECT ... FOR UPDATE внутри
// открытой транзакции. Здесь не проверяется сама блокировка (для этого нужен
// второй параллельный конкурентный запрос — см. TODO ниже), а то, что запрос
// корректно выполняется через unwrap(tx) и возвращает актуальные данные.
func TestMenuRepo_GetItemsForUpdate_WithinTransaction(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня FOR UPDATE", "Казань", uniqueAPIKeyHash())
	menuRepo := postgres.NewMenuRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	id1, err := menuRepo.CreateItem(ctx, domain.MenuItem{VenueID: venueID, Name: "Item 1", PriceKopecks: 1000, Currency: "RUB"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	id2, err := menuRepo.CreateItem(ctx, domain.MenuItem{VenueID: venueID, Name: "Item 2", PriceKopecks: 2000, Currency: "RUB"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tx, err := txManager.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	items, err := menuRepo.GetItemsForUpdate(ctx, tx, []string{id1, id2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(items), items)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestMenuRepo_GetItemsForUpdate_BlocksConcurrentTransaction —
// защита от гонки, ради которой в CreateOrder используется SELECT ... FOR
// UPDATE: если два клиента одновременно пытаются заказать последнюю
// доступную позицию, вторая транзакция обязана ДОЖДАТЬСЯ завершения первой,
// а не прочитать устаревшее is_available=true параллельно.
//
// Схема теста:
//  1. tx1 открывает транзакцию, блокирует строку через GetItemsForUpdate.
//  2. Параллельно (в горутине) tx2 пытается сделать то же самое над той же
//     строкой — это должно ЗАБЛОКИРОВАТЬСЯ на уровне Postgres.
//  3. Мы проверяем, что tx2 не завершается, пока жив tx1 (через select с
//     небольшим таймаутом — она не должна успеть).
//  4. tx1 обновляет is_available=false и коммитит.
//  5. Только теперь tx2 разблокируется и должна увидеть уже актуальное
//     (обновлённое) значение is_available — ровно то поведение, которое
//     не даёт продать один и тот же товар дважды.
func TestMenuRepo_GetItemsForUpdate_BlocksConcurrentTransaction(t *testing.T) {
	ctx := context.Background()
	venueID := insertVenue(t, ctx, "Кухня FOR UPDATE конкурентность", "Казань", uniqueAPIKeyHash())
	menuRepo := postgres.NewMenuRepo(testPool)
	txManager := postgres.NewTxManager(testPool)

	itemID, err := menuRepo.CreateItem(ctx, domain.MenuItem{
		VenueID: venueID, Name: "Последний стейк", PriceKopecks: 89000, Currency: "RUB",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// --- tx1: захватываем блокировку сырой транзакцией и держим её открытой ---
	rawTx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer rawTx1.Rollback(ctx) //nolint:errcheck

	if _, err := rawTx1.Exec(ctx, `SELECT id FROM menu_items WHERE id = $1 FOR UPDATE`, itemID); err != nil {
		t.Fatalf("tx1: lock row: %v", err)
	}

	// --- tx2: параллельно пытаемся заблокировать ту же строку продакшн-путём ---
	tx2Started := make(chan struct{})
	tx2Done := make(chan []domain.MenuItem, 1)
	tx2Err := make(chan error, 1)

	go func() {
		tx2, err := txManager.Begin(ctx)
		if err != nil {
			tx2Err <- err
			return
		}
		defer tx2.Rollback(ctx) //nolint:errcheck

		close(tx2Started)
		items, err := menuRepo.GetItemsForUpdate(ctx, tx2, []string{itemID}) // должна заблокироваться здесь
		if err != nil {
			tx2Err <- err
			return
		}
		tx2Done <- items
	}()

	<-tx2Started
	// Даём горутине tx2 шанс дойти до FOR UPDATE и реально заблокироваться.
	select {
	case <-tx2Done:
		t.Fatal("tx2 завершилась ДО коммита tx1 — блокировка FOR UPDATE не сработала, гонка не защищена")
	case err := <-tx2Err:
		t.Fatalf("tx2 неожиданно упала до коммита tx1: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Ожидаемо: tx2 всё ещё ждёт снятия блокировки — продолжаем тест.
	}

	// --- tx1: меняем данные В ТОЙ ЖЕ транзакции и коммитим, снимая блокировку ---
	if _, err := rawTx1.Exec(ctx, `UPDATE menu_items SET is_available = false, updated_at = now() WHERE id = $1`, itemID); err != nil {
		t.Fatalf("tx1: update: %v", err)
	}
	if err := rawTx1.Commit(ctx); err != nil {
		t.Fatalf("tx1: commit: %v", err)
	}

	// --- Теперь tx2 обязана разблокироваться и увидеть уже актуальные данные ---
	select {
	case items := <-tx2Done:
		if len(items) != 1 {
			t.Fatalf("expected 1 item after unblocking, got %d", len(items))
		}
		if items[0].IsAvailable {
			t.Error("ожидалось, что tx2 увидит is_available=false, закоммиченное в tx1 " +
				"(если увидела true — блокировка не защитила от чтения устаревших данных)")
		}
	case err := <-tx2Err:
		t.Fatalf("tx2 failed after tx1 commit: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for tx2 to unblock after tx1 commit")
	}
}
