// Package kitchen эмулирует работу настоящей кухни: получает новые заказы
// (через poll или webhook) и с задержками переводит их по статусам
// ACCEPTED -> COOKING -> READY -> DELIVERING -> COMPLETED, вызывая Venue API core.
// Это и есть "пример интеграции заведения" из ТЗ.
package kitchen

import (
	"log/slog"
	"math/rand"
	"time"

	"github.com/avito/kuhnya/restaurant-service/internal/coreclient"
)

type Emulator struct {
	core *coreclient.Client
	log  *slog.Logger
}

func New(core *coreclient.Client, log *slog.Logger) *Emulator {
	return &Emulator{core: core, log: log}
}

// sleepFunc/delayFunc вынесены в переменные пакета намеренно — это позволяет
// тестам подменить их на мгновенные (см. emulator_test.go) и не ждать реальные
// 2-5 секунд на каждый шаг. В проде используются реальные time.Sleep/randomDelay.
var (
	sleepFunc = time.Sleep
	delayFunc = randomDelay
)

// HandleNewOrder запускает полный жизненный цикл обработки одного заказа
// в отдельной горутине — не блокирует вызывающий код (poller или webhook-хендлер).
func (e *Emulator) HandleNewOrder(orderID string) {
	go func() {
		if err := e.core.AcceptOrder(orderID); err != nil {
			e.log.Error("accept order failed", "order_id", orderID, "error", err)
			return
		}
		e.log.Info("order accepted", "order_id", orderID)

		steps := []string{"COOKING", "READY", "DELIVERING", "COMPLETED"}
		for _, status := range steps {
			sleepFunc(delayFunc())
			if err := e.core.UpdateStatus(orderID, status); err != nil {
				e.log.Error("update status failed", "order_id", orderID, "status", status, "error", err)
				return
			}
			e.log.Info("order status updated", "order_id", orderID, "status", status)
		}
	}()
}

func randomDelay() time.Duration {
	//nolint:gosec // Эмуляция задержки кухни, криптографическая стойкость не требуется
	return time.Duration(2+rand.Intn(4)) * time.Second
}

// StartPoller — fallback-режим: периодически спрашивает core о новых заказах
// (на случай если webhook-доставка недоступна). Используется вместе с
// internal/webhook, а не вместо него.
func (e *Emulator) StartPoller(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	seen := make(map[string]bool)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			orders, err := e.core.ListNewOrders()
			if err != nil {
				e.log.Warn("poll new orders failed", "error", err)
				continue
			}
			for _, o := range orders {
				if seen[o.ID] {
					continue
				}
				seen[o.ID] = true
				e.log.Info("new order discovered via polling", "order_id", o.ID)
				e.HandleNewOrder(o.ID)
			}
		}
	}
}
