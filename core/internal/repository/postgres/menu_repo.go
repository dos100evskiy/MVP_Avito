package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/avito/kuhnya/core/internal/domain"
)

type MenuRepo struct {
	pool *pgxpool.Pool
}

func NewMenuRepo(pool *pgxpool.Pool) *MenuRepo {
	return &MenuRepo{pool: pool}
}

func (r *MenuRepo) ListCategories(ctx context.Context, venueID string) ([]domain.MenuCategory, error) {
	const q = `SELECT id, venue_id, name, sort_order FROM menu_categories WHERE venue_id = $1 ORDER BY sort_order`
	rows, err := r.pool.Query(ctx, q, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MenuCategory
	for rows.Next() {
		var c domain.MenuCategory
		if err := rows.Scan(&c.ID, &c.VenueID, &c.Name, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *MenuRepo) CreateCategory(ctx context.Context, c domain.MenuCategory) (string, error) {
	const q = `INSERT INTO menu_categories (venue_id, name, sort_order) VALUES ($1, $2, $3) RETURNING id`
	var id string
	err := r.pool.QueryRow(ctx, q, c.VenueID, c.Name, c.SortOrder).Scan(&id)
	return id, err
}

func (r *MenuRepo) ListItems(ctx context.Context, venueID string) ([]domain.MenuItem, error) {
	const q = `
		SELECT id, venue_id, category_id, name, coalesce(description,''), price_Kopecks, currency, is_available, created_at, updated_at
		FROM menu_items WHERE venue_id = $1 ORDER BY name`
	rows, err := r.pool.Query(ctx, q, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MenuItem
	for rows.Next() {
		it, err := scanMenuItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (r *MenuRepo) GetItem(ctx context.Context, id string) (*domain.MenuItem, error) {
	const q = `
		SELECT id, venue_id, category_id, name, coalesce(description,''), price_Kopecks, currency, is_available, created_at, updated_at
		FROM menu_items WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	it, err := scanMenuItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMenuItemNotFound
	}
	return it, err
}

// GetItemsForUpdate — читает позиции меню с блокировкой строк в рамках
// открытой транзакции создания заказа (см. usecase.OrderUseCase.CreateOrder).
func (r *MenuRepo) GetItemsForUpdate(ctx context.Context, tx domain.Tx, ids []string) ([]domain.MenuItem, error) {
	const q = `
		SELECT id, venue_id, category_id, name, coalesce(description,''), price_Kopecks, currency, is_available, created_at, updated_at
		FROM menu_items WHERE id = ANY($1) FOR UPDATE`
	rows, err := unwrap(tx).Query(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MenuItem
	for rows.Next() {
		it, err := scanMenuItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (r *MenuRepo) CreateItem(ctx context.Context, item domain.MenuItem) (string, error) {
	const q = `
		INSERT INTO menu_items (venue_id, category_id, name, description, price_Kopecks, currency, is_available)
		VALUES ($1, $2, $3, $4, $5, $6, true) RETURNING id`
	var id string
	err := r.pool.QueryRow(ctx, q, item.VenueID, item.CategoryID, item.Name, item.Description, item.PriceKopecks, item.Currency).Scan(&id)
	return id, err
}

func (r *MenuRepo) UpdateItem(ctx context.Context, item domain.MenuItem) error {
	const q = `
		UPDATE menu_items SET name = $2, description = $3, price_Kopecks = $4, currency = $5, updated_at = now()
		WHERE id = $1`
	_, err := r.pool.Exec(ctx, q, item.ID, item.Name, item.Description, item.PriceKopecks, item.Currency)
	return err
}

func (r *MenuRepo) SetAvailability(ctx context.Context, id string, available bool) error {
	const q = `UPDATE menu_items SET is_available = $2, updated_at = now() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id, available)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMenuItemNotFound
	}
	return nil
}

func scanMenuItem(row rowScanner) (*domain.MenuItem, error) {
	var it domain.MenuItem
	var categoryID *string
	err := row.Scan(&it.ID, &it.VenueID, &categoryID, &it.Name, &it.Description, &it.PriceKopecks, &it.Currency, &it.IsAvailable, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, err
	}
	it.CategoryID = categoryID
	return &it, nil
}
