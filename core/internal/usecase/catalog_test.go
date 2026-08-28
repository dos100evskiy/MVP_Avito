package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/avito/kuhnya/core/internal/domain"
	"github.com/avito/kuhnya/core/internal/usecase"
)

func TestGetVenueMenu_NotFound(t *testing.T) {
	venues := &venueRepoMock{byID: map[string]domain.Venue{}}
	menu := &menuRepoMock{items: map[string]domain.MenuItem{}}
	uc := usecase.NewCatalogUseCase(venues, menu)

	_, err := uc.GetVenueMenu(context.Background(), "unknown-venue")
	if !errors.Is(err, domain.ErrVenueNotFound) {
		t.Fatalf("expected ErrVenueNotFound, got %v", err)
	}
}

func TestGetVenueMenu_Success(t *testing.T) {
	venues := &venueRepoMock{byID: map[string]domain.Venue{
		"venue-1": {ID: "venue-1", Name: "Тестовая кухня", City: "Москва"},
	}}
	menu := &menuRepoMock{items: map[string]domain.MenuItem{
		"item-1": {ID: "item-1", VenueID: "venue-1", Name: "Борщ", IsAvailable: true},
		"item-2": {ID: "item-2", VenueID: "venue-2", Name: "Чужое блюдо", IsAvailable: true},
	}}
	uc := usecase.NewCatalogUseCase(venues, menu)

	got, err := uc.GetVenueMenu(context.Background(), "venue-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "item-1" {
		t.Errorf("expected only venue-1 items, got %+v", got.Items)
	}
}
