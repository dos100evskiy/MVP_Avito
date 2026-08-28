// Package postgres — адаптер репозиториев domain-интерфейсов поверх pgx/v5.
// Здесь же — реализация domain.Tx / domain.TxManager, чтобы usecase-слой
// мог управлять границами транзакции, не зная о pgx напрямую.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/avito/kuhnya/core/internal/domain"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

type pgxTx struct {
	tx pgx.Tx
}

func (t *pgxTx) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *pgxTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

func (m *TxManager) Begin(ctx context.Context) (domain.Tx, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxTx{tx: tx}, nil
}

// unwrap достаёт нативный pgx.Tx из domain.Tx — используется репозиториями,
// которым нужно выполнить запрос именно в рамках открытой транзакции
// (например, GetItemsForUpdate с SELECT ... FOR UPDATE).
func unwrap(tx domain.Tx) pgx.Tx {
	return tx.(*pgxTx).tx
}
