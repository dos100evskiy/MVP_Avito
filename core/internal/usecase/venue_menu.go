package usecase

import (
	"context"

	"github.com/avito/kuhnya/core/internal/domain"
)

// VenueMenuUseCase — сценарии со стороны заведения: управление своим меню.
// Отдельно от CatalogUseCase, т.к. права доступа и контракт другие (Venue API vs Client API).
type VenueMenuUseCase struct {
	menu domain.MenuRepository
}

func NewVenueMenuUseCase(m domain.MenuRepository) *VenueMenuUseCase {
	return &VenueMenuUseCase{menu: m}
}

func (uc *VenueMenuUseCase) CreateCategory(ctx context.Context, c domain.MenuCategory) (string, error) {
	return uc.menu.CreateCategory(ctx, c)
}

func (uc *VenueMenuUseCase) CreateItem(ctx context.Context, item domain.MenuItem) (string, error) {
	item.IsAvailable = true
	return uc.menu.CreateItem(ctx, item)
}

func (uc *VenueMenuUseCase) UpdateItem(ctx context.Context, item domain.MenuItem) error {
	return uc.menu.UpdateItem(ctx, item)
}

func (uc *VenueMenuUseCase) SetAvailability(ctx context.Context, itemID string, available bool) error {
	return uc.menu.SetAvailability(ctx, itemID, available)
}
