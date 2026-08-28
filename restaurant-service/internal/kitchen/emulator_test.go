// Тесты в том же пакете kitchen (whitebox), а не kitchen_test — нужен доступ
// к неэкспортированным package-level переменным sleepFunc/delayFunc, чтобы
// подменить реальные задержки 2-5с на мгновенные (см. emulator.go).
package kitchen

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avito/kuhnya/restaurant-service/internal/coreclient"
)

// withInstantDelays подменяет sleepFunc/delayFunc на мгновенные на время теста
// и восстанавливает оригиналы по завершении — иначе тесты реально ждали бы
// по 2-5 секунд на каждый из 4 переходов статуса.
func withInstantDelays(t *testing.T) {
	t.Helper()
	origSleep, origDelay := sleepFunc, delayFunc
	sleepFunc = func(time.Duration) {}
	delayFunc = func() time.Duration { return 0 }
	t.Cleanup(func() {
		sleepFunc, delayFunc = origSleep, origDelay
	})
}

type recorder struct {
	mu    sync.Mutex
	calls []string // "METHOD PATH" в порядке поступления
}

func (r *recorder) add(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, method+" "+path)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// waitForCalls ждёт, пока recorder не накопит ровно n вызовов, либо истечёт
// таймаут — нужно, т.к. HandleNewOrder работает в отдельной горутине.
func waitForCalls(t *testing.T, rec *recorder, n int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if calls := rec.snapshot(); len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d calls, got %v", n, rec.snapshot())
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHandleNewOrder_FullHappyPath(t *testing.T) {
	withInstantDelays(t)
	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	core := coreclient.New(srv.URL, "dev-venue-key")
	e := New(core, testLogger())

	e.HandleNewOrder("order-1")

	// accept + 4 перехода статуса = 5 запросов к core
	calls := waitForCalls(t, rec, 5, 2*time.Second)

	want := []string{
		"POST /orders/order-1/accept",
		"PATCH /orders/order-1/status",
		"PATCH /orders/order-1/status",
		"PATCH /orders/order-1/status",
		"PATCH /orders/order-1/status",
	}
	if len(calls) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(calls), calls)
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("call %d: expected %q, got %q", i, w, calls[i])
		}
	}
}

// TestHandleNewOrder_AcceptFails_StopsPipeline проверяет, что при ошибке на
// этапе accept дальнейшие переходы статуса не запускаются — заказ не должен
// "телепортироваться" в READY, если заведение не смогло его даже принять.
func TestHandleNewOrder_AcceptFails_StopsPipeline(t *testing.T) {
	withInstantDelays(t)
	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError) // accept всегда падает
	}))
	defer srv.Close()

	core := coreclient.New(srv.URL, "dev-venue-key")
	e := New(core, testLogger())

	e.HandleNewOrder("order-1")

	// Ждём немного и проверяем, что запрос был ровно один (сам accept) —
	// т.к. по контракту второго запроса не должно случиться, waitForCalls
	// тут не годится (мы ждём ОТСУТСТВИЯ вызовов), поэтому просто пауза.
	time.Sleep(100 * time.Millisecond)

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 call (accept only), got %d: %v", len(calls), calls)
	}
	if calls[0] != "POST /orders/order-1/accept" {
		t.Errorf("unexpected call: %s", calls[0])
	}
}

func TestStartPoller_DiscoversAndProcessesNewOrder(t *testing.T) {
	withInstantDelays(t)
	rec := &recorder{}
	var served bool
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.Method, r.URL.Path)

		if r.Method == http.MethodGet {
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if !served {
				served = true
				_ = json.NewEncoder(w).Encode([]coreclient.Order{{ID: "order-1", VenueID: "venue-1", Status: "CREATED"}})
			} else {
				_ = json.NewEncoder(w).Encode([]coreclient.Order{}) // дальше новых заказов нет
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	core := coreclient.New(srv.URL, "dev-venue-key")
	e := New(core, testLogger())

	stop := make(chan struct{})
	defer close(stop)
	go e.StartPoller(10*time.Millisecond, stop)

	// Ждём: 1 GET (poll, где нашёлся заказ) + POST accept + 4 PATCH статуса.
	// GET-ов может быть больше одного (поллер тикает каждые 10мс), поэтому
	// проверяем не точное количество, а факт, что accept+статусы долетели.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls := rec.snapshot()
		acceptSeen := false
		statusCount := 0
		for _, c := range calls {
			if c == "POST /orders/order-1/accept" {
				acceptSeen = true
			}
			if c == "PATCH /orders/order-1/status" {
				statusCount++
			}
		}
		if acceptSeen && statusCount == 4 {
			return // успех
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: poller did not process discovered order, calls=%v", rec.snapshot())
}
