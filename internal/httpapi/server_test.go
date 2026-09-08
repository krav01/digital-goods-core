package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krav01/digital-goods-core/internal/catalog"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/payment"
	"github.com/krav01/digital-goods-core/internal/supplier"
)

type stubStore struct{ readyErr error }
type catalogStore struct{ stubStore }

func (s stubStore) Ready(context.Context) error { return s.readyErr }
func (s stubStore) CreateOrder(context.Context, string, string) (order.Order, bool, error) {
	return order.Order{}, false, order.ErrNotFound
}
func (s stubStore) GetOrder(context.Context, string) (order.Order, error) {
	return order.Order{}, order.ErrNotFound
}
func (s stubStore) AcceptPayment(context.Context, payment.Event) error      { return nil }
func (s stubStore) ListProducts(context.Context) ([]catalog.Product, error) { return nil, nil }
func (catalogStore) ListProducts(context.Context) ([]catalog.Product, error) {
	return []catalog.Product{{SKU: "SKU"}}, nil
}

type inventoryFunc func(context.Context) ([]supplier.Inventory, error)

func (f inventoryFunc) Inventory(ctx context.Context) ([]supplier.Inventory, error) { return f(ctx) }

func TestNewHandlerStatusEndpoints(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			NewHandler(stubStore{}).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want JSON", contentType)
			}
			if body := response.Body.String(); body != "{\"status\":\"ok\"}\n" {
				t.Errorf("body = %q, want status JSON", body)
			}
		})
	}
}

func TestNewHandlerRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"unknown_order", "GET", "/orders/missing", "", 404},
		{"missing_sku", "POST", "/orders", "{}", 400},
		{"unknown_field", "POST", "/orders", `{"sku":"STEAM-TOPUP-500","price":1}`, 400},
		{"trailing_json", "POST", "/orders", `{"sku":"A"} {}`, 400},
		{"large_body", "POST", "/orders", `{"sku":"` + strings.Repeat("x", 17000) + `"}`, 400},
		{"missing_event", "POST", "/webhooks/payment", `{}`, 400},
		{"invalid_status", "POST", "/webhooks/payment", `{"event_id":"e","order_id":"o","amount":500,"currency":"RUB","status":"refunded","created_at":"2026-09-07T00:00:00Z"}`, 400},
		{"wrong_method", "POST", "/healthz", "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			NewHandler(stubStore{}).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.status {
				t.Fatalf("HTTP %d, want %d; %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestReadinessFailureDoesNotAffectHealth(t *testing.T) {
	t.Parallel()
	h := NewHandler(stubStore{readyErr: errors.New("unavailable")})
	for _, tc := range []struct {
		path string
		want int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestProductsRequireCompleteInventory(t *testing.T) {
	good := inventoryFunc(func(context.Context) ([]supplier.Inventory, error) {
		return []supplier.Inventory{{SKU: "SKU", Available: 2}}, nil
	})
	second := inventoryFunc(func(context.Context) ([]supplier.Inventory, error) {
		return []supplier.Inventory{{SKU: "SKU", Available: 3}}, nil
	})
	w := httptest.NewRecorder()
	NewHandler(catalogStore{}, good, second).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/products", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":5`) {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
	bad := inventoryFunc(func(context.Context) ([]supplier.Inventory, error) { return nil, errors.New("down") })
	w = httptest.NewRecorder()
	NewHandler(catalogStore{}, good, bad).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/products", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
