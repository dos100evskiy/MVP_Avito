package domain_test

import (
	"testing"

	"github.com/avito/kuhnya/core/internal/domain"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to domain.OrderStatus
		want     bool
	}{
		{domain.OrderStatusCreated, domain.OrderStatusAccepted, true},
		{domain.OrderStatusCreated, domain.OrderStatusCancelled, true},
		{domain.OrderStatusCreated, domain.OrderStatusReady, false}, // нельзя перепрыгнуть этапы
		{domain.OrderStatusAccepted, domain.OrderStatusCooking, true},
		{domain.OrderStatusAccepted, domain.OrderStatusCancelled, true},
		{domain.OrderStatusCooking, domain.OrderStatusReady, true},
		{domain.OrderStatusCooking, domain.OrderStatusCancelled, false}, // после ACCEPTED отмена уже недоступна
		{domain.OrderStatusReady, domain.OrderStatusDelivering, true},
		{domain.OrderStatusDelivering, domain.OrderStatusCompleted, true},
		{domain.OrderStatusCompleted, domain.OrderStatusCancelled, false}, // финальный статус
		{domain.OrderStatusCancelled, domain.OrderStatusAccepted, false},  // финальный статус
	}

	for _, c := range cases {
		got := domain.CanTransition(c.from, c.to)
		if got != c.want {
			t.Errorf("CanTransition(%s -> %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
