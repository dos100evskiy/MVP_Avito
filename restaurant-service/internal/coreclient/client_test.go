package coreclient_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/avito/kuhnya/restaurant-service/internal/coreclient"
)

// recordedRequest — то, что реально долетело до "core" в тестовом HTTP-сервере.
// Проверяем не только код ответа, но и метод/путь/заголовки/тело — это и есть
// контракт с Venue API core-сервиса, который coreclient обязан соблюдать.
type recordedRequest struct {
	Method string
	Path   string
	APIKey string
	Body   string
}

func newRecordingServer(t *testing.T, status int, respBody string) (*httptest.Server, *[]recordedRequest, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recordedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, recordedRequest{
			Method: r.Method,
			Path:   r.URL.RequestURI(),
			APIKey: r.Header.Get("X-API-Key"),
			Body:   string(body),
		})
		mu.Unlock()

		if r.Header.Get("Content-Type") != "" && r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected Content-Type: %s", r.Header.Get("Content-Type"))
		}

		w.WriteHeader(status)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	return srv, &reqs, &mu
}

func TestClient_AcceptOrder(t *testing.T) {
	srv, reqs, mu := newRecordingServer(t, http.StatusNoContent, "")
	defer srv.Close()

	c := coreclient.New(srv.URL, "dev-venue-key")
	if err := c.AcceptOrder("order-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*reqs))
	}
	got := (*reqs)[0]
	if got.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", got.Method)
	}
	if got.Path != "/orders/order-1/accept" {
		t.Errorf("unexpected path: %s", got.Path)
	}
	if got.APIKey != "dev-venue-key" {
		t.Errorf("expected X-API-Key header to be forwarded, got %q", got.APIKey)
	}
}

func TestClient_UpdateStatus(t *testing.T) {
	srv, reqs, mu := newRecordingServer(t, http.StatusNoContent, "")
	defer srv.Close()

	c := coreclient.New(srv.URL, "dev-venue-key")
	if err := c.UpdateStatus("order-1", "COOKING"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	got := (*reqs)[0]
	if got.Method != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", got.Method)
	}
	if got.Path != "/orders/order-1/status" {
		t.Errorf("unexpected path: %s", got.Path)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload["status"] != "COOKING" {
		t.Errorf("expected status=COOKING in body, got %+v", payload)
	}
}

func TestClient_RejectOrder(t *testing.T) {
	srv, reqs, mu := newRecordingServer(t, http.StatusNoContent, "")
	defer srv.Close()

	c := coreclient.New(srv.URL, "dev-venue-key")
	if err := c.RejectOrder("order-1", "out of stock"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	got := (*reqs)[0]
	if !strings.Contains(got.Body, "out of stock") {
		t.Errorf("expected reason in body, got %s", got.Body)
	}
}

func TestClient_ListNewOrders(t *testing.T) {
	respJSON := `[{"id":"order-1","venueId":"venue-1","status":"CREATED","totalKopecks":39000,"currency":"RUB","customerRef":"me"}]`
	srv, reqs, mu := newRecordingServer(t, http.StatusOK, respJSON)
	defer srv.Close()

	c := coreclient.New(srv.URL, "dev-venue-key")
	orders, err := c.ListNewOrders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 1 || orders[0].ID != "order-1" {
		t.Fatalf("unexpected orders: %+v", orders)
	}

	mu.Lock()
	defer mu.Unlock()
	got := (*reqs)[0]
	if got.Method != http.MethodGet {
		t.Errorf("expected GET, got %s", got.Method)
	}
	if got.Path != "/orders?status=CREATED" {
		t.Errorf("unexpected path: %s", got.Path)
	}
}

func TestClient_UnexpectedStatus_ReturnsError(t *testing.T) {
	srv, _, _ := newRecordingServer(t, http.StatusInternalServerError, "")
	defer srv.Close()

	c := coreclient.New(srv.URL, "dev-venue-key")
	if err := c.AcceptOrder("order-1"); err == nil {
		t.Fatal("expected error on unexpected status code, got nil")
	}
}
