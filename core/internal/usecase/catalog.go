package usecase

import (
	"context"

	"github.com/avito/kuhnya/core/internal/domain"
)

// CatalogUseCase отвечает за клиентский сценарий "посмотреть заведения и меню".
type CatalogUseCase struct {
	venues domain.VenueRepository
	menu   domain.MenuRepository
}

func NewCatalogUseCase(v domain.VenueRepository, m domain.MenuRepository) *CatalogUseCase {
	return &CatalogUseCase{venues: v, menu: m}
}

func (uc *CatalogUseCase) ListVenues(ctx context.Context, city string, limit, offset int) ([]domain.Venue, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return uc.venues.List(ctx, city, limit, offset)
}

type VenueMenu struct {
	Venue      domain.Venue
	Categories []domain.MenuCategory
	Items      []domain.MenuItem
}

func (uc *CatalogUseCase) GetVenueMenu(ctx context.Context, venueID string) (*VenueMenu, error) {
	venue, err := uc.venues.GetByID(ctx, venueID)
	if err != nil {
		return nil, err
	}
	categories, err := uc.menu.ListCategories(ctx, venueID)
	if err != nil {
		return nil, err
	}
	items, err := uc.menu.ListItems(ctx, venueID)
	if err != nil {
		return nil, err
	}
	return &VenueMenu{Venue: *venue, Categories: categories, Items: items}, nil
}
