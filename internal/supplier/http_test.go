package supplier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krav01/digital-goods-core/internal/delivery"
)

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
