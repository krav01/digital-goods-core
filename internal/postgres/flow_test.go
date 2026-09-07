//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/httpapi"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/payment"
	"github.com/krav01/digital-goods-core/internal/postgres"
	"github.com/krav01/digital-goods-core/internal/supplier"
)

type fixture struct {
	app, sup *pgxpool.Pool
	store    *postgres.Store
	issuer   *postgres.SupplierStore
	worker   *delivery.Worker
	api      *httptest.Server
}

// Only generated databases ending in _test are created/dropped. Existing databases are never reset.
func database(t *testing.T, scope string) (*pgxpool.Pool, string) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Fatal("TEST_DATABASE_URL must point to a dedicated PostgreSQL test server")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.Connect(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := order.NewID("dgc_" + scope)
	if err != nil {
		t.Fatal(err)
	}
	name := suffix + "_test"
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if err := admin.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, raw)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if err := conn.Close(ctx); err != nil {
				t.Error(err)
			}
		}()
		if !strings.HasPrefix(name, "dgc_") || !strings.HasSuffix(name, "_test") {
			t.Fatal("unsafe cleanup name")
		}
		if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	if err := postgres.Migrate(u.String(), scope); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(u.String(), scope); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	pool, err := postgres.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Ready(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	return pool, u.String()
}

func setup(t *testing.T) *fixture {
	t.Helper()
	app, _ := database(t, "app")
	sup, _ := database(t, "supplier")
	f := &fixture{app: app, sup: sup, store: postgres.New(app), issuer: postgres.NewSupplier(sup)}
	srv := httptest.NewServer(supplier.NewHandler(f.issuer))
	t.Cleanup(srv.Close)
	f.api = httptest.NewServer(httpapi.NewHandler(f.store))
	t.Cleanup(f.api.Close)
	f.worker = delivery.NewWorker(f.store, supplier.NewClient(srv.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f
}

func request(t *testing.T, base, method, path string, input, output any, want int) {
	t.Helper()
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, base+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: HTTP %d, want %d; %s", method, path, resp.StatusCode, want, data)
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil {
			t.Fatal(err)
		}
	}
}

func event(id, oid string) payment.Event {
	return payment.Event{ID: id, OrderID: oid, Status: "paid", AmountMinor: 50000, Currency: "RUB", CreatedAt: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)}
}

func create(t *testing.T, f *fixture, id, sku string) {
	t.Helper()
	if _, _, err := f.store.CreateOrder(t.Context(), id, sku); err != nil {
		t.Fatal(err)
	}
}

func paid(t *testing.T, f *fixture, id string) {
	t.Helper()
	create(t, f, id, "STEAM-TOPUP-500")
	if err := f.store.AcceptPayment(t.Context(), event("evt_"+id, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ProcessPayment(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func step(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("%s = %d, want %d", query, n, want)
	}
}

func TestHTTPOrderPaymentDelivery(t *testing.T) {
	f := setup(t)
	request(t, f.api.URL, "GET", "/readyz", nil, nil, 200)
	var o order.Order
	request(t, f.api.URL, "POST", "/orders", map[string]string{"sku": "STEAM-TOPUP-500", "order_id": "ord_http"}, &o, 201)
	if o.PriceMinor != 50000 || o.Status != "created" || o.Code != "" {
		t.Fatalf("unexpected order: %+v", o)
	}
	request(t, f.api.URL, "POST", "/orders", map[string]string{"sku": "STEAM-TOPUP-500", "order_id": o.ID}, nil, 200)
	request(t, f.api.URL, "POST", "/orders", map[string]string{"sku": "KEY-GTA5", "order_id": o.ID}, nil, 409)
	payload := map[string]any{"event_id": "evt_http", "order_id": o.ID, "status": "paid", "amount": 500, "currency": "RUB", "created_at": "2026-09-07T00:00:00Z"}
	request(t, f.api.URL, "POST", "/webhooks/payment", payload, nil, 200)
	request(t, f.api.URL, "POST", "/webhooks/payment", payload, nil, 200)
	step(t, f)
	request(t, f.api.URL, "GET", "/orders/"+o.ID, nil, &o, 200)
	if o.Status != "delivered" || o.Code == "" || o.PaidAt == nil {
		t.Fatalf("not delivered: %+v", o)
	}
	request(t, f.api.URL, "POST", "/webhooks/payment", payload, nil, 200)
	step(t, f)
	count(t, f.app, "SELECT count(*) FROM deliveries", 1)
	count(t, f.app, "SELECT count(*) FROM payment_events", 1)
	count(t, f.sup, "SELECT count(*) FROM inventory_keys WHERE issued_request_id IS NOT NULL", 1)
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
	payload["amount"] = 501
	request(t, f.api.URL, "POST", "/webhooks/payment", payload, nil, 409)
}

func TestEarlyPayment(t *testing.T) {
	f := setup(t)
	if err := f.store.AcceptPayment(t.Context(), event("evt_early", "ord_early")); err != nil {
		t.Fatal(err)
	}
	step(t, f)
	count(t, f.app, "SELECT count(*) FROM payment_events WHERE processing_state='waiting_order'", 1)
	create(t, f, "ord_early", "STEAM-TOPUP-500")
	// Make the durable schedule due without real sleeps.
	if _, err := f.app.Exec(t.Context(), "UPDATE payment_events SET next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	step(t, f)
	o, err := f.store.GetOrder(t.Context(), "ord_early")
	if err != nil || o.Status != "delivered" {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestInvalidPaymentAndTerminalConflicts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		events     []payment.Event
		status     string
		deliveries int
		state      string
	}{
		{"wrong_amount", []payment.Event{func() payment.Event { e := event("e1", "o"); e.AmountMinor = 1; return e }()}, "created", 0, "rejected"},
		{"wrong_currency", []payment.Event{func() payment.Event { e := event("e1", "o"); e.Currency = "USD"; return e }()}, "created", 0, "rejected"},
		{"paid_then_failed", []payment.Event{event("e1", "o"), func() payment.Event { e := event("e2", "o"); e.Status = "failed"; return e }()}, "delivered", 1, "conflict"},
		{"failed_then_paid", []payment.Event{func() payment.Event { e := event("e1", "o"); e.Status = "failed"; return e }(), event("e2", "o")}, "payment_failed", 0, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			create(t, f, "o", "STEAM-TOPUP-500")
			for _, e := range tc.events {
				if err := f.store.AcceptPayment(t.Context(), e); err != nil {
					t.Fatal(err)
				}
				step(t, f)
			}
			o, err := f.store.GetOrder(t.Context(), "o")
			if err != nil || o.Status != tc.status {
				t.Fatalf("%+v %v", o, err)
			}
			count(t, f.app, "SELECT count(*) FROM deliveries", tc.deliveries)
			var state string
			if err := f.app.QueryRow(t.Context(), "SELECT processing_state FROM payment_events WHERE event_id=$1", tc.events[len(tc.events)-1].ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != tc.state {
				t.Fatalf("got state %s, want %s", state, tc.state)
			}
		})
	}
}

func TestSupplierReplayAndRefusalSurviveReconnect(t *testing.T) {
	sup, dsn := database(t, "supplier")
	store := postgres.NewSupplier(sup)
	req := delivery.Request{RequestID: "req_one", OrderID: "ord_one", SKU: "STEAM-TOPUP-500"}
	first, err := store.Issue(t.Context(), req)
	if err != nil || first.Status != "ok" {
		t.Fatalf("%+v %v", first, err)
	}
	sup.Close()
	sup, err = postgres.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close()
	store = postgres.NewSupplier(sup)
	replay, err := store.Issue(t.Context(), req)
	if err != nil || replay != first {
		t.Fatalf("replay %+v %v, want %+v", replay, err, first)
	}
	req.SKU = "KEY-GTA5"
	if _, err := store.Issue(t.Context(), req); !errors.Is(err, order.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	req = delivery.Request{RequestID: "req_empty", OrderID: "ord_empty", SKU: "EMPTY"}
	empty, err := store.Issue(t.Context(), req)
	if err != nil || !empty.Final || empty.Reason != "out_of_stock" {
		t.Fatalf("%+v %v", empty, err)
	}
	if _, err := sup.Exec(t.Context(), "INSERT INTO inventory_keys(code,sku) VALUES ('TEST-RESTOCK','EMPTY')"); err != nil {
		t.Fatal(err)
	}
	replay, err = store.Issue(t.Context(), req)
	if err != nil || replay != empty {
		t.Fatalf("refusal changed: %+v %v", replay, err)
	}
	req.RequestID = "req_after_restock"
	restock, err := store.Issue(t.Context(), req)
	if err != nil || restock.Code != "TEST-RESTOCK" {
		t.Fatalf("%+v %v", restock, err)
	}
}

func TestRecoveryAfterSupplierCommitBeforeLocalCommit(t *testing.T) {
	f := setup(t)
	paid(t, f, "ord_crash")
	lease, ok, err := f.store.ClaimDelivery(t.Context(), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	req, err := f.store.PrepareDelivery(t.Context(), lease)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := f.issuer.Issue(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Model a dead worker after external commit: do not apply the response; expire its lease.
	if _, err := f.app.Exec(t.Context(), "UPDATE delivery_jobs SET leased_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	step(t, f)
	o, err := f.store.GetOrder(t.Context(), "ord_crash")
	if err != nil || o.Code != issued.Code || o.Status != "delivered" {
		t.Fatalf("%+v %v", o, err)
	}
	if err := f.store.FinishDelivery(t.Context(), lease, req, issued, 0); !errors.Is(err, delivery.ErrLeaseLost) {
		t.Fatalf("old worker result accepted: %v", err)
	}
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
	count(t, f.app, "SELECT count(*) FROM delivery_operations", 1)
}

func TestOutOfStockRestoresAfterRestock(t *testing.T) {
	f := setup(t)
	if _, err := f.sup.Exec(t.Context(), "DELETE FROM inventory_keys WHERE sku='STEAM-TOPUP-500'"); err != nil {
		t.Fatal(err)
	}
	paid(t, f, "ord_empty")
	step(t, f)
	o, err := f.store.GetOrder(t.Context(), "ord_empty")
	if err != nil || o.Status != "out_of_stock" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := f.sup.Exec(t.Context(), "INSERT INTO inventory_keys(code,sku) VALUES ('TEST-RESTOCK','STEAM-TOPUP-500')"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Exec(t.Context(), "UPDATE delivery_jobs SET available_at=now()"); err != nil {
		t.Fatal(err)
	}
	step(t, f)
	o, err = f.store.GetOrder(t.Context(), "ord_empty")
	if err != nil || o.Code != "TEST-RESTOCK" || o.Status != "delivered" {
		t.Fatalf("%+v %v", o, err)
	}
	count(t, f.app, "SELECT count(*) FROM delivery_operations", 2)
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
}
