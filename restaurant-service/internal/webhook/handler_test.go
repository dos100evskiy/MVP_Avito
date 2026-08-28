package webhook_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avito/kuhnya/restaurant-service/internal/coreclient"
	"github.com/avito/kuhnya/restaurant-service/internal/kitchen"
	"github.com/avito/kuhnya/restaurant-service/internal/webhook"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewHandler_ValidPayload_TriggersProcessing(t *testing.T) {
	var mu sync.Mutex
	var acceptCalled bool

	// "Core" API, на который эмулятор кухни пойдёт после получения вебхука.
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/accept") {
			mu.Lock()
			acceptCalled = true
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer coreSrv.Close()

	core := coreclient.New(coreSrv.URL, "dev-venue-key")
	emulator := kitchen.New(core, testLogger())
	handler := webhook.NewHandler(emulator, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/internal/orders",
		strings.NewReader(`{"orderId":"order-1","venueId":"venue-1","status":"CREATED"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", rec.Code)
	}

	// HandleNewOrder внутри эмулятора асинхронный — ждём, пока долетит accept.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		called := acceptCalled
		mu.Unlock()
		if called {
			return // успех
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for emulator to call accept on core")
}

func TestNewHandler_InvalidPayload_Returns400(t *testing.T) {
	emulator := kitchen.New(coreclient.New("http://unused.invalid", "key"), testLogger())
	handler := webhook.NewHandler(emulator, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/internal/orders", strings.NewReader(`not-json`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}
