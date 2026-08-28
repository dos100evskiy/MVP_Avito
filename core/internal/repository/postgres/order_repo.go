package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/avito/kuhnya/core/internal/domain"
)

type OrderRepo struct {
	pool *pgxpool.Pool
}

func NewOrderRepo(pool *pgxpool.Pool) *OrderRepo {
	return &OrderRepo{pool: pool}
}

// Create создаёт заказ и его позиции одним запросом на каждую сущность,
// в рамках транзакции, открытой usecase-слоем (см. domain.TxManager).
func (r *OrderRepo) Create(ctx context.Context, tx domain.Tx, order domain.Order) (string, error) {
	pgtx := unwrap(tx)

	const qOrder = `
		INSERT INTO orders (id, venue_id, status, total_Kopecks, currency, customer_ref)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := pgtx.Exec(ctx, qOrder, order.ID, order.VenueID, order.Status, order.TotalKopecks, order.Currency, order.CustomerRef); err != nil {
		return "", err
	}

	const qItem = `
		INSERT INTO order_items (id, order_id, menu_item_id, name_snapshot, price_snapshot, quantity)
		VALUES ($1, $2, $3, $4, $5, $6)`
	batch := &pgx.Batch{}
	for _, it := range order.Items {
		batch.Queue(qItem, it.ID, order.ID, it.MenuItemID, it.NameSnapshot, it.PriceSnapshot, it.Quantity)
	}
	br := pgtx.SendBatch(ctx, batch)
	defer br.Close()
	for range order.Items {
		if _, err := br.Exec(); err != nil {
			return "", err
		}
	}

	return order.ID, nil
}

func (r *OrderRepo) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	const qOrder = `
		SELECT id, venue_id, status, total_Kopecks, currency, coalesce(customer_ref,''), created_at, updated_at
		FROM orders WHERE id = $1`
	row := r.pool.QueryRow(ctx, qOrder, id)

	var o domain.Order
	var status string
	err := row.Scan(&o.ID, &o.VenueID, &status, &o.TotalKopecks, &o.Currency, &o.CustomerRef, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Status = domain.OrderStatus(status)

	const qItems = `
		SELECT id, order_id, menu_item_id, name_snapshot, price_snapshot, quantity
		FROM order_items WHERE order_id = $1`
	rows, err := r.pool.Query(ctx, qItems, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it domain.OrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.NameSnapshot, &it.PriceSnapshot, &it.Quantity); err != nil {
			return nil, err
		}
		o.Items = append(o.Items, it)
	}
	return &o, rows.Err()
}

func (r *OrderRepo) ListByVenue(ctx context.Context, venueID string, status domain.OrderStatus, limit, offset int) ([]domain.Order, error) {
	const q = `
		SELECT id, venue_id, status, total_Kopecks, currency, coalesce(customer_ref,''), created_at, updated_at
		FROM orders
		WHERE venue_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, q, venueID, string(status), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Order
	for rows.Next() {
		var o domain.Order
		var st string
		if err := rows.Scan(&o.ID, &o.VenueID, &st, &o.TotalKopecks, &o.Currency, &o.CustomerRef, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Status = domain.OrderStatus(st)
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpdateStatus атомарно проверяет текущий статус (WHERE status = $from) и меняет его —
// дополнительная защита от гонки поверх проверки в usecase (например, два
// параллельных запроса venue accept + client cancel на один заказ).
func (r *OrderRepo) UpdateStatus(ctx context.Context, id string, from, to domain.OrderStatus) error {
	const q = `UPDATE orders SET status = $3, updated_at = now() WHERE id = $1 AND status = $2`
	tag, err := r.pool.Exec(ctx, q, id, string(from), string(to))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidTransition
	}
	return nil
}

// AppendStatusHistory — если tx передана (не nil), запись идёт в рамках той же
// транзакции, что и создание/изменение заказа (обязательно для CreateOrder,
// иначе вставка упадёт по внешнему ключу, т.к. заказ ещё не закоммичен и не
// виден другим соединениям из пула). Если tx == nil — обычная запись через пул
// (используется при точечной смене статуса вне транзакции, например accept/cancel).
func (r *OrderRepo) AppendStatusHistory(ctx context.Context, tx domain.Tx, orderID string, from, to domain.OrderStatus, changedBy string) error {
	const q = `INSERT INTO order_status_history (order_id, from_status, to_status, changed_by) VALUES ($1, $2, $3, $4)`
	var fromVal *string
	if from != "" {
		s := string(from)
		fromVal = &s
	}
	if tx != nil {
		_, err := unwrap(tx).Exec(ctx, q, orderID, fromVal, string(to), changedBy)
		return err
	}
	_, err := r.pool.Exec(ctx, q, orderID, fromVal, string(to), changedBy)
	return err
}
