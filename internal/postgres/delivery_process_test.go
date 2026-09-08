//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/postgres"
)

type processFixture struct {
	app, sup, supB           *pgxpool.Pool
	appDSN, supDSN, supBDSN  string
	store                    *postgres.Store
	api, supplier, supplierB *childProcess
}

func processSetup(t *testing.T) *processFixture {
	t.Helper()
	f := new(processFixture)
	f.app, f.appDSN = database(t, "app")
	f.sup, f.supDSN = database(t, "supplier")
	f.supB, f.supBDSN = database(t, "supplier-b")
	f.store = postgres.New(f.app)
	f.supplier = startProcess(t, "supplier", f.supDSN, "", "", "")
	f.supplierB = startProcess(t, "supplier", f.supBDSN, "", "", "")
	f.api = startProcess(t, "api", f.appDSN, "", "", "")
	return f
}

func (f *processFixture) worker(t *testing.T, phase string) *childProcess {
	t.Helper()
	return startProcess(t, "worker", f.appDSN, f.supplier.url, f.supplierB.url, phase)
}

func (f *processFixture) create(t *testing.T, id string) {
	t.Helper()
	request(t, f.api.url, "POST", "/orders", map[string]string{"sku": "STEAM-TOPUP-500", "order_id": id}, nil, 201)
}

func payload(id, eventID string) map[string]any {
	return map[string]any{"event_id": eventID, "order_id": id, "status": "paid", "amount": 500,
		"currency": "RUB", "created_at": "2026-09-07T00:00:00Z"}
}

func sqlExec(t *testing.T, pool *pgxpool.Pool, query string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

// Poll a durable predicate under a bounded deadline, not a guessed sleep.
func awaitCount(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var got int
	for {
		if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if got == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: got %d, want %d: %v", query, got, want, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f *processFixture) oneEffect(t *testing.T, events int) {
	t.Helper()
	count(t, f.app, "SELECT count(*) FROM payment_events", events)
	count(t, f.app, "SELECT count(*) FROM deliveries", 1)
	count(t, f.app, "SELECT count(*) FROM delivery_jobs", 1)
	count(t, f.app, "SELECT count(*) FROM delivery_jobs WHERE state='done'", 1)
	count(t, f.app, "SELECT count(*) FROM delivery_operations", 1)
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
	count(t, f.sup, "SELECT count(*) FROM inventory_keys WHERE issued_request_id IS NOT NULL", 1)
	var requestID, code string
	if err := f.app.QueryRow(t.Context(), "SELECT request_id,code FROM deliveries").Scan(&requestID, &code); err != nil {
		t.Fatal(err)
	}
	var matches int
	if err := f.sup.QueryRow(t.Context(), `SELECT count(*) FROM issue_requests r JOIN inventory_keys k
		ON k.issued_request_id=r.request_id AND k.code=r.code
		WHERE r.request_id=$1 AND r.code=$2 AND r.outcome='issued'`, requestID, code).Scan(&matches); err != nil || matches != 1 {
		t.Fatalf("application/supplier effects disagree: matches=%d, err=%v", matches, err)
	}
}

// Do not call Fatal/FailNow inside request goroutines; collect all outcomes first.
func concurrently(t *testing.T, n int, fn func(int) error) {
	t.Helper()
	start := make(chan struct{})
	errorsCh := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errorsCh <- fn(i)
		})
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

func post(ctx context.Context, endpoint string, input any, want int) ([]byte, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	closeErr := resp.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if resp.StatusCode != want {
		return nil, fmt.Errorf("POST %s: HTTP %d, want %d", endpoint, resp.StatusCode, want)
	}
	return data, nil
}

func TestConcurrentWebhooksProcesses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unique bool
		early  bool
	}{
		{name: "same_event"},
		{name: "distinct_events", unique: true},
		{name: "before_order", unique: true, early: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := processSetup(t)
			if !tc.early {
				f.create(t, "ord_race")
			}
			workers := make([]*childProcess, 4)
			for i := range workers {
				workers[i] = f.worker(t, "")
			}
			for _, w := range workers {
				w.resume(t)
			}
			send := func(i int) error {
				id := "evt_same"
				if tc.unique {
					id = fmt.Sprintf("evt_%d", i)
				}
				_, err := post(t.Context(), f.api.url+"/webhooks/payment", payload("ord_race", id), 200)
				return err
			}
			concurrently(t, 50, send)
			wantEvents := 1
			if tc.unique {
				wantEvents = 50
			}
			for _, w := range workers {
				w.await(t, "claim_checked") // Every independent worker attempted a claim.
			}
			if tc.early {
				awaitCount(t, f.app, "SELECT count(*) FROM payment_events WHERE processing_state='waiting_order'", 50)
				f.create(t, "ord_race")
			}
			awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
			awaitCount(t, f.app, "SELECT count(*) FROM payment_events WHERE processing_state='applied'", wantEvents)
			before, err := f.store.GetOrder(t.Context(), "ord_race")
			if err != nil || before.PaidAt == nil {
				t.Fatalf("paid order missing: %v", err)
			}
			concurrently(t, 50, send) // Repeat the entire burst after delivery.
			for _, w := range workers {
				w.stop(t)
			}
			after, err := f.store.GetOrder(t.Context(), "ord_race")
			if err != nil || after.Code == "" || after.Code != before.Code || after.Status != "delivered" || after.PaidAt == nil || !after.PaidAt.Equal(*before.PaidAt) {
				t.Fatalf("replay changed the delivery/payment: %v", err)
			}
			f.oneEffect(t, wantEvents)
		})
	}
}

func TestConcurrentSupplierRequests(t *testing.T) {
	f := processSetup(t)
	req := delivery.Request{RequestID: "req_shared", OrderID: "ord_shared", SKU: "STEAM-TOPUP-500"}
	results := make([]delivery.Result, 50)
	concurrently(t, 50, func(i int) error {
		data, err := post(t.Context(), f.supplier.url+"/issue", req, 200)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, &results[i])
	})
	for _, result := range results {
		if result.Status != "ok" || result.Code == "" || result != results[0] {
			t.Fatal("concurrent supplier replies disagree")
		}
	}
	concurrently(t, 50, func(i int) error {
		conflict := req
		switch i % 3 {
		case 0:
			conflict.OrderID = "ord_other"
		case 1:
			conflict.SKU = "KEY-GTA5"
		case 2:
			conflict.RequestID = fmt.Sprintf("req_other_%d", i)
		}
		_, err := post(t.Context(), f.supplier.url+"/issue", conflict, 409)
		return err
	})
	count(t, f.sup, "SELECT count(*) FROM issue_requests", 1)
	count(t, f.sup, "SELECT count(*) FROM inventory_keys WHERE issued_request_id IS NOT NULL", 1)
}

func TestConcurrentLastKeyProcesses(t *testing.T) {
	f := processSetup(t)
	sqlExec(t, f.sup, `DELETE FROM inventory_keys WHERE sku='STEAM-TOPUP-500'
		AND code<>(SELECT min(code) FROM inventory_keys WHERE sku='STEAM-TOPUP-500')`)
	sqlExec(t, f.supB, "DELETE FROM inventory_keys WHERE sku='STEAM-TOPUP-500'")
	for _, id := range []string{"ord_one", "ord_two"} {
		f.create(t, id)
		request(t, f.api.url, "POST", "/webhooks/payment", payload(id, "evt_"+id), nil, 200)
	}
	workers := []*childProcess{f.worker(t, "after_prepare"), f.worker(t, "after_prepare")}
	for _, w := range workers {
		w.resume(t)
	}
	for _, w := range workers {
		w.await(t, "checkpoint") // Both orders are prepared by different processes.
	}
	for _, w := range workers {
		w.resume(t) // Release the competing supplier requests together.
	}
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='out_of_stock'", 1)
	for _, w := range workers {
		w.stop(t)
	}
	count(t, f.sup, "SELECT count(*) FROM inventory_keys WHERE issued_request_id IS NOT NULL", 1)
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
	count(t, f.app, "SELECT count(*) FROM deliveries", 1)
	sqlExec(t, f.sup, "INSERT INTO inventory_keys(code,sku) VALUES ('TEST-LAST-KEY-RESTOCK','STEAM-TOPUP-500')")
	sqlExec(t, f.app, "UPDATE delivery_jobs SET available_at=now() WHERE state='ready'")
	recovery := f.worker(t, "")
	recovery.resume(t)
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 2)
	recovery.stop(t)
	count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 2)
	count(t, f.sup, "SELECT count(*) FROM inventory_keys WHERE issued_request_id IS NOT NULL", 2)
	count(t, f.app, "SELECT count(DISTINCT code) FROM deliveries", 2)
}

func TestProcessUnknownSupplierResultDoesNotFallback(t *testing.T) {
	f := processSetup(t)
	f.supplier.kill(t)
	t.Setenv("SUPPLIER_AFTER_ISSUE_DELAY", "3s")
	f.supplier = startProcess(t, "supplier", f.supDSN, "", "", "")
	f.create(t, "ord_unknown_a")
	request(t, f.api.url, "POST", "/webhooks/payment", payload("ord_unknown_a", "evt_unknown_a"), nil, 200)
	w := f.worker(t, "")
	w.resume(t)
	awaitCount(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", 1)
	awaitCount(t, f.app, "SELECT count(*) FROM delivery_operations WHERE supplier='A' AND state='unknown'", 1)
	w.stop(t)
	count(t, f.supB, "SELECT count(*) FROM issue_requests", 0)
	count(t, f.app, "SELECT count(*) FROM deliveries", 0)
}

func TestProcessCrashRecovery(t *testing.T) {
	for _, phase := range []string{"after_payment", "after_prepare", "before_finish"} {
		t.Run(phase, func(t *testing.T) {
			f := processSetup(t)
			f.create(t, "ord_crash")
			request(t, f.api.url, "POST", "/webhooks/payment", payload("ord_crash", "evt_crash"), nil, 200)
			w := f.worker(t, phase)
			w.resume(t)
			w.await(t, "checkpoint")
			count(t, f.app, "SELECT count(*) FROM deliveries", 0)
			count(t, f.app, "SELECT count(*) FROM delivery_jobs", 1)
			wantOperations, wantIssued := 1, 0
			if phase == "after_payment" {
				wantOperations = 0
			}
			if phase == "before_finish" {
				wantIssued = 1
			}
			count(t, f.app, "SELECT count(*) FROM delivery_operations", wantOperations)
			count(t, f.sup, "SELECT count(*) FROM issue_requests WHERE outcome='issued'", wantIssued)
			w.kill(t)
			if phase == "before_finish" {
				f.supplier.kill(t)
				f.supplier = startProcess(t, "supplier", f.supDSN, "", "", "")
				// Accelerate only this scenario; after_prepare below uses the real 15s lease.
				sqlExec(t, f.app, "UPDATE delivery_jobs SET leased_until=now()-interval '1 second' WHERE state='leased'")
			}
			replacement := f.worker(t, "")
			replacement.resume(t)
			awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
			replacement.stop(t)
			f.oneEffect(t, 1)
		})
	}
}

func TestProcessStaleLeaseResult(t *testing.T) {
	f := processSetup(t)
	f.create(t, "ord_stale")
	request(t, f.api.url, "POST", "/webhooks/payment", payload("ord_stale", "evt_stale"), nil, 200)
	old := f.worker(t, "before_finish")
	old.resume(t)
	old.await(t, "checkpoint")
	sqlExec(t, f.app, "UPDATE delivery_jobs SET leased_until=now()-interval '1 second'")
	current := f.worker(t, "")
	current.resume(t)
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
	old.resume(t)
	old.await(t, "lease_lost") // Assert the actual old process's FinishDelivery result.
	old.stop(t)
	current.stop(t)
	f.oneEffect(t, 1)
	count(t, f.app, "SELECT count(*) FROM delivery_jobs WHERE lease_version=2", 1)
}

func TestProcessAPICrashAfterAcceptance(t *testing.T) {
	f := processSetup(t)
	f.create(t, "ord_api")
	request(t, f.api.url, "POST", "/webhooks/payment", payload("ord_api", "evt_api"), nil, 200)
	f.api.kill(t)
	count(t, f.app, "SELECT count(*) FROM payment_events WHERE processing_state='pending'", 1)
	f.api = startProcess(t, "api", f.appDSN, "", "", "")
	var o order.Order
	request(t, f.api.url, "GET", "/orders/ord_api", nil, &o, 200)
	if o.Status != "created" {
		t.Fatalf("unexpected status before worker: %s", o.Status)
	}
	w := f.worker(t, "")
	w.resume(t)
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
	w.stop(t)
	f.oneEffect(t, 1)
}

func TestProcessCrashBeforePaymentCommit(t *testing.T) {
	f := processSetup(t)
	f.create(t, "ord_lock")
	request(t, f.api.url, "POST", "/webhooks/payment", payload("ord_lock", "evt_lock"), nil, 200)
	tx, err := f.app.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	if _, err := tx.Exec(t.Context(), "SELECT id FROM orders WHERE id='ord_lock' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	w := f.worker(t, "")
	w.resume(t)
	awaitCount(t, f.app, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
		AND wait_event_type='Lock' AND query LIKE 'SELECT id,price_minor,currency,status,paid_at%'`, 1)
	w.kill(t)
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	count(t, f.app, "SELECT count(*) FROM orders WHERE status='created' AND paid_at IS NULL", 1)
	count(t, f.app, "SELECT count(*) FROM payment_events WHERE processing_state='pending'", 1)
	count(t, f.app, "SELECT count(*) FROM delivery_jobs", 0)
	replacement := f.worker(t, "")
	replacement.resume(t)
	awaitCount(t, f.app, "SELECT count(*) FROM orders WHERE status='delivered'", 1)
	replacement.stop(t)
	f.oneEffect(t, 1)
}
