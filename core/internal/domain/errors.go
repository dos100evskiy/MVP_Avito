package domain

import "errors"

// Ошибки домена. Транспортный слой сам решает, в какой HTTP-код их превращать
// (см. internal/transport/http/apierror.go), usecase-слой ничего не знает про HTTP.
var (
	ErrVenueNotFound       = errors.New("venue not found")
	ErrMenuItemNotFound    = errors.New("menu item not found")
	ErrOrderNotFound       = errors.New("order not found")
	ErrItemUnavailable     = errors.New("menu item is not available")
	ErrItemsFromDiffVenues = errors.New("all order items must belong to the same venue")
	ErrInvalidTransition   = errors.New("invalid order status transition")
	ErrUnauthorized        = errors.New("invalid or missing api key")
	ErrEmptyOrder          = errors.New("order must contain at least one item")
)

// UnavailableItem описывает одну позицию, недоступную на момент оформления заказа.
// Возвращается usecase-слоем как часть составной ошибки, чтобы клиент получил
// не просто 409, а конкретный список проблемных позиций (см. CJM: "конфликт наличия").
type UnavailableItem struct {
	MenuItemID string
	Reason     string
}

type ErrItemsUnavailable struct {
	Items []UnavailableItem
}

func (e *ErrItemsUnavailable) Error() string {
	return "one or more order items are unavailable"
}
