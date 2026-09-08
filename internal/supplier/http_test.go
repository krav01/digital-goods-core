package supplier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krav01/digital-goods-core/internal/delivery"
)

type storeFunc struct {
	issue     func(context.Context, delivery.Request) (delivery.Result, error)
	refuse    func(context.Context, delivery.Request) (delivery.Result, error)
	inventory func(context.Context) ([]Inventory, error)
}

func (s storeFunc) Issue(ctx context.Context, req delivery.Request) (delivery.Result, error) {
	return s.issue(ctx, req)
}

func (s storeFunc) Refuse(ctx context.Context, req delivery.Request) (delivery.Result, error) {
	return s.refuse(ctx, req)
}

func (s storeFunc) Inventory(ctx context.Context) ([]Inventory, error) {
	if s.inventory == nil {
		return nil, nil
	}
	return s.inventory(ctx)
}

func (storeFunc) Ready(context.Context) error { return nil }

func TestClientIssue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		valid  bool
	}{
		{"success", 200, `{"status":"ok","request_id":"req","code":"code"}`, true},
		{"refusal", 409, `{"status":"error","request_id":"req","reason":"out_of_stock","final":true}`, true},
		{"ordinary_503", 503, `{"status":"error","request_id":"req","reason":"unavailable"}`, false},
		{"incorrect_id", 200, `{"status":"ok","request_id":"other","code":"code"}`, false},
		{"empty_code", 200, `{"status":"ok","request_id":"req"}`, false},
		{"invalid_json", 200, `<html>`, false},
		{"trailing_json", 200, `{"status":"ok","request_id":"req","code":"code"}{}`, false},
		{"redirect", 302, `{"status":"ok","request_id":"req","code":"code"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/issue" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			_, err := NewClient(srv.URL).Issue(t.Context(), delivery.Request{RequestID: "req", OrderID: "ord", SKU: "sku"})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
}

func TestClientIssueCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := NewClient("http://127.0.0.1:1").Issue(ctx, delivery.Request{RequestID: "req"})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestHandlerInventory(t *testing.T) {
	handler := NewHandler(storeFunc{inventory: func(context.Context) ([]Inventory, error) {
		return []Inventory{{SKU: "SKU_A", Available: 2}, {SKU: "SKU_B", Available: 0}}, nil
	}})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/inventory", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var inventory []Inventory
	if err := json.NewDecoder(recorder.Body).Decode(&inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 2 || inventory[0].SKU != "SKU_A" || inventory[0].Available != 2 {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestHandlerRandomFinalUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rate       int
		wantStatus int
		wantRefuse bool
	}{
		{name: "disabled", rate: 0, wantStatus: http.StatusOK},
		{name: "always", rate: 100, wantStatus: http.StatusConflict, wantRefuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused := false
			handler := NewHandler(storeFunc{
				issue: func(_ context.Context, req delivery.Request) (delivery.Result, error) {
					return delivery.Result{Status: "ok", RequestID: req.RequestID, Code: "code"}, nil
				},
				refuse: func(_ context.Context, req delivery.Request) (delivery.Result, error) {
					refused = true
					return delivery.Result{Status: "error", RequestID: req.RequestID, Reason: "unavailable", Final: true}, nil
				},
			}, WithRandomFinalUnavailable(tc.rate))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(`{"request_id":"req","order_id":"ord","sku":"sku"}`))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if refused != tc.wantRefuse {
				t.Fatalf("refused = %v, want %v", refused, tc.wantRefuse)
			}
			var result delivery.Result
			if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.RequestID != "req" {
				t.Errorf("request ID = %q, want req", result.RequestID)
			}
		})
	}
}
