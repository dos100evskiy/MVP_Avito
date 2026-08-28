//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/repository/postgres"
)

func TestVenueRepo_GetByID_Success(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewVenueRepo(testPool)

	hash := uniqueAPIKeyHash()
	id := insertVenue(t, ctx, "Тестовая кухня GetByID", "Казань", hash)

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != id {
		t.Errorf("expected id %s, got %s", id, got.ID)
	}
	if got.City != "Казань" {
		t.Errorf("expected city Казань, got %s", got.City)
	}
	if got.APIKeyHash != hash {
		t.Errorf("expected api key hash %s, got %s", hash, got.APIKeyHash)
	}
	if !got.IsActive {
		t.Error("expected venue to be active")
	}
}

func TestVenueRepo_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewVenueRepo(testPool)

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrVenueNotFound) {
		t.Fatalf("expected ErrVenueNotFound, got %v", err)
	}
}

func TestVenueRepo_GetByAPIKeyHash(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewVenueRepo(testPool)

	hash := uniqueAPIKeyHash()
	id := insertVenue(t, ctx, "Тестовая кухня APIKey", "Уфа", hash)

	got, err := repo.GetByAPIKeyHash(ctx, hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != id {
		t.Errorf("expected id %s, got %s", id, got.ID)
	}

	_, err = repo.GetByAPIKeyHash(ctx, "definitely-not-a-real-hash")
	if !errors.Is(err, domain.ErrVenueNotFound) {
		t.Fatalf("expected ErrVenueNotFound for unknown hash, got %v", err)
	}
}

func TestVenueRepo_List_FiltersByCity(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewVenueRepo(testPool)

	cityMarker := "УникальныйГород-" + uniqueAPIKeyHash() // город тоже делаем уникальным для изоляции теста
	id1 := insertVenue(t, ctx, "Заведение 1", cityMarker, uniqueAPIKeyHash())
	id2 := insertVenue(t, ctx, "Заведение 2", cityMarker, uniqueAPIKeyHash())
	// В другом городе — не должно попасть в выборку
	insertVenue(t, ctx, "Заведение из другого города", "Другой-"+uniqueAPIKeyHash(), uniqueAPIKeyHash())

	got, err := repo.List(ctx, cityMarker, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 venues in city %s, got %d: %+v", cityMarker, len(got), got)
	}
	ids := map[string]bool{got[0].ID: true, got[1].ID: true}
	if !ids[id1] || !ids[id2] {
		t.Errorf("expected venues %s and %s in result, got %+v", id1, id2, got)
	}
}
