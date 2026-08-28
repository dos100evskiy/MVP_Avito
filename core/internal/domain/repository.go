package domain

import "context"

// Репозитории описаны как интерфейсы в domain-пакете (порт), а реализация
// лежит в internal/repository/postgres (адаптер) — классическая ports & adapters,
// это позволяет тестировать usecase-слой на моках без поднятия реальной БД.

type VenueRepository interface {
	GetByID(ctx context.Context, id string) (*Venue, error)
	GetByAPIKeyHash(ctx context.Context, hash string) (*Venue, error)
	List(ctx context.Context, city string, limit, offset int) ([]Venue, error)
}

type MenuRepository interface {
	ListCategories(ctx context.Context, venueID string) ([]MenuCategory, error)
	CreateCategory(ctx context.Context, c MenuCategory) (string, error)

	ListItems(ctx context.Context, venueID string) ([]MenuItem, error)
	GetItem(ctx context.Context, id string) (*MenuItem, error)
	// GetItemsForUpdate читает позиции меню с блокировкой строк (SELECT ... FOR UPDATE)
	// в рамках уже открытой транзакции — используется при создании заказа,
	// чтобы исключить гонку между проверкой наличия и списанием.
	GetItemsForUpdate(ctx context.Context, tx Tx, ids []string) ([]MenuItem, error)
	CreateItem(ctx context.Context, item MenuItem) (string, error)
	UpdateItem(ctx context.Context, item MenuItem) error
	SetAvailability(ctx context.Context, id string, available bool) error
}

type OrderRepository interface {
	Create(ctx context.Context, tx Tx, order Order) (string, error)
	GetByID(ctx context.Context, id string) (*Order, error)
	ListByVenue(ctx context.Context, venueID string, status OrderStatus, limit, offset int) ([]Order, error)
	UpdateStatus(ctx context.Context, id string, from, to OrderStatus) error
	// AppendStatusHistory пишет запись истории статуса. tx может быть nil —
	// тогда запись идёт обычным соединением из пула; если передана открытая
	// транзакция (см. CreateOrder), запись обязана идти через неё же, иначе
	// заказ может быть ещё не закоммичен и вставка упадёт по внешнему ключу.
	AppendStatusHistory(ctx context.Context, tx Tx, orderID string, from, to OrderStatus, changedBy string) error
}

// Tx — абстракция над транзакцией БД, чтобы usecase мог управлять границами
// транзакции (Begin/Commit/Rollback), не зная о конкретном драйвере (pgx).
type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type TxManager interface {
	Begin(ctx context.Context) (Tx, error)
}
