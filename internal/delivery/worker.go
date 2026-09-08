package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"
)

var ErrLeaseLost = errors.New("delivery lease lost")

type Request struct {
	RequestID string `json:"request_id"`
	OrderID   string `json:"order_id"`
	SKU       string `json:"sku"`
	Supplier  string `json:"-"`
}

type Result struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Final     bool   `json:"final,omitempty"`
}

type Lease struct {
	OrderID  string
	Version  int64
	Attempts int
}

type Repository interface {
	ProcessPayment(context.Context) (bool, error)
	ClaimDelivery(context.Context, time.Duration) (Lease, bool, error)
	PrepareDelivery(context.Context, Lease) (Request, error)
	FinishDelivery(context.Context, Lease, Request, Result, time.Duration) error
}

type Issuer interface {
	Issue(context.Context, Request) (Result, error)
}

type Worker struct {
	store   Repository
	issuers map[string]Issuer
	logger  *slog.Logger
	intn    func(int) int
}

func NewWorker(store Repository, issuer Issuer, logger *slog.Logger) *Worker {
	return NewWorkerWithSuppliers(store, map[string]Issuer{"A": issuer}, logger)
}

func NewWorkerWithSuppliers(store Repository, issuers map[string]Issuer, logger *slog.Logger) *Worker {
	return &Worker{store: store, issuers: issuers, logger: logger, intn: rand.IntN}
}

func (w *Worker) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		worked, err := w.Step(ctx)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // Caller cancellation is a normal worker shutdown, not a process failure.
		}
		if err != nil {
			w.logger.Error("worker step failed", "error", err)
		}
		if !worked || err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}

// Step processes bounded local work and at most one external operation.
func (w *Worker) Step(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	processed, err := w.store.ProcessPayment(ctx)
	if err != nil {
		return false, fmt.Errorf("process inbox: %w", err)
	}
	lease, found, err := w.store.ClaimDelivery(ctx, 15*time.Second)
	if err != nil || !found {
		return processed, err
	}
	req, err := w.store.PrepareDelivery(ctx, lease)
	if err != nil {
		return true, err
	}
	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
	issuer, ok := w.issuers[req.Supplier]
	if !ok {
		callCancel()
		return true, fmt.Errorf("supplier %q is not configured", req.Supplier)
	}
	result, callErr := issuer.Issue(callCtx, req)
	callCancel()
	if callErr != nil {
		// A transport error does not prove absence of a supplier-side commit.
		result = Result{Status: "unknown", RequestID: req.RequestID, Reason: "supplier_result_unknown"}
	}
	delay := retryDelay(lease.Attempts, w.intn)
	if err := w.store.FinishDelivery(ctx, lease, req, result, delay); err != nil {
		return true, err
	}
	w.logger.Info("delivery attempt recorded", "order_id", req.OrderID, "request_id", req.RequestID,
		"supplier", req.Supplier, "attempt", lease.Attempts, "result", result.Status, "reason", result.Reason)
	return true, nil
}

func retryDelay(attempt int, intn func(int) int) time.Duration {
	base := time.Second * time.Duration(1<<min(max(attempt-1, 0), 5))
	half := base / 2
	return half + time.Duration(intn(int(half.Milliseconds())+1))*time.Millisecond
}
