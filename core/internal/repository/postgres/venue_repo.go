package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/avito/kuhnya/core/internal/domain"
)

type VenueRepo struct {
	pool *pgxpool.Pool
}

func NewVenueRepo(pool *pgxpool.Pool) *VenueRepo {
	return &VenueRepo{pool: pool}
}

func (r *VenueRepo) GetByID(ctx context.Context, id string) (*domain.Venue, error) {
	const q = `
		SELECT id, name, coalesce(description, ''), city, is_active, api_key_hash, coalesce(callback_url, ''), created_at
		FROM venues WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	return scanVenue(row)
}

func (r *VenueRepo) GetByAPIKeyHash(ctx context.Context, hash string) (*domain.Venue, error) {
	const q = `
		SELECT id, name, coalesce(description, ''), city, is_active, api_key_hash, coalesce(callback_url, ''), created_at
		FROM venues WHERE api_key_hash = $1`
	row := r.pool.QueryRow(ctx, q, hash)
	return scanVenue(row)
}

func (r *VenueRepo) List(ctx context.Context, city string, limit, offset int) ([]domain.Venue, error) {
	const q = `
		SELECT id, name, coalesce(description, ''), city, is_active, api_key_hash, coalesce(callback_url, ''), created_at
		FROM venues
		WHERE is_active = true AND ($1 = '' OR city = $1)
		ORDER BY name
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, city, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Venue
	for rows.Next() {
		v, err := scanVenueRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVenue(row pgx.Row) (*domain.Venue, error) {
	return scanVenueRow(row)
}

func scanVenueRow(row rowScanner) (*domain.Venue, error) {
	var v domain.Venue
	err := row.Scan(&v.ID, &v.Name, &v.Description, &v.City, &v.IsActive, &v.APIKeyHash, &v.CallbackURL, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrVenueNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
