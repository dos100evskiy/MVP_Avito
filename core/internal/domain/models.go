package domain

import "time"

// OrderStatus — статус заказа, конечный автомат вида
// CREATED -> ACCEPTED -> COOKING -> READY -> DELIVERING -> COMPLETED
// с возможным переходом в CANCELLED из CREATED/ACCEPTED.
type OrderStatus string

const (
	OrderStatusCreated    OrderStatus = "CREATED"
	OrderStatusAccepted   OrderStatus = "ACCEPTED"
	OrderStatusCooking    OrderStatus = "COOKING"
	OrderStatusReady      OrderStatus = "READY"
	OrderStatusDelivering OrderStatus = "DELIVERING"
	OrderStatusCompleted  OrderStatus = "COMPLETED"
	OrderStatusCancelled  OrderStatus = "CANCELLED"
)

// allowedTransitions описывает разрешённые переходы статусов заказа.
// Вынесено отдельной картой, чтобы бизнес-правило проверялось в одном месте (usecase),
// а не размазывалось по хендлерам.
var allowedTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusCreated:    {OrderStatusAccepted, OrderStatusCancelled},
	OrderStatusAccepted:   {OrderStatusCooking, OrderStatusCancelled},
	OrderStatusCooking:    {OrderStatusReady},
	OrderStatusReady:      {OrderStatusDelivering},
	OrderStatusDelivering: {OrderStatusCompleted},
}

// CanTransition проверяет, разрешён ли переход из from в to.
func CanTransition(from, to OrderStatus) bool {
	for _, s := range allowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

type Venue struct {
	ID          string
	Name        string
	Description string
	City        string
	IsActive    bool
	APIKeyHash  string
	CallbackURL string
	CreatedAt   time.Time
}

type MenuCategory struct {
	ID        string
	VenueID   string
	Name      string
	SortOrder int
}

type MenuItem struct {
	ID           string
	VenueID      string
	CategoryID   *string
	Name         string
	Description  string
	PriceKopecks int64
	Currency     string
	IsAvailable  bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type OrderItem struct {
	ID            string
	OrderID       string
	MenuItemID    string
	NameSnapshot  string
	PriceSnapshot int64
	Quantity      int
}

type Order struct {
	ID           string
	VenueID      string
	Status       OrderStatus
	TotalKopecks int64
	Currency     string
	CustomerRef  string
	Items        []OrderItem
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewOrderItemInput — то, что присылает клиент при создании заказа:
// ссылка на позицию меню и количество. Цена и название на этом этапе
// не принимаются от клиента и всегда берутся из БД (защита от подмены цены).
type NewOrderItemInput struct {
	MenuItemID string
	Quantity   int
}
