//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// insertVenue — фикстура заведения через прямой SQL-инсерт, а не через
// domain.VenueRepository: создание заведений сознательно не входит в MVP-скоуп
// API (см. README/план — список заведений закрытый и админится вручную), у
// репозитория нет метода Create для venues. Для теста это нормально —
// VenueRepo здесь не участник, а лишь то, что мы тестируем через Get*/List.
// apiKeyHash не обязан быть настоящим sha256-хешем — колонка обычный text,
// уникальность важнее реалистичности значения.
func insertVenue(t *testing.T, ctx context.Context, name, city, apiKeyHash string) string {
	t.Helper()
	id := uuid.NewString()
	const q = `INSERT INTO venues (id, name, city, is_active, api_key_hash) VALUES ($1, $2, $3, true, $4)`
	if _, err := testPool.Exec(ctx, q, id, name, city, apiKeyHash); err != nil {
		t.Fatalf("insert venue fixture: %v", err)
	}
	return id
}

// uniqueAPIKeyHash — короткий хелпер, чтобы не дублировать uuid.NewString()
// на каждый вызов insertVenue (важно из-за UNIQUE(api_key_hash) в схеме).
func uniqueAPIKeyHash() string {
	return "test-hash-" + uuid.NewString()
}
