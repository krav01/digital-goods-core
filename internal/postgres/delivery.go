package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/order"
)

func (s *Store) ClaimDelivery(ctx context.Context, duration time.Duration) (delivery.Lease, bool, error) {
	var lease delivery.Lease
	err := s.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT order_id FROM delivery_jobs WHERE (state='ready' AND available_at<=now())
		OR (state='leased' AND leased_until<=now()) ORDER BY available_at,order_id
		LIMIT 1 FOR UPDATE SKIP LOCKED)
		UPDATE delivery_jobs j SET state='leased',leased_until=now()+$1::bigint*interval '1 millisecond',
		lease_version=lease_version+1,attempts=attempts+1 FROM candidate c WHERE j.order_id=c.order_id
		RETURNING j.order_id,j.lease_version,j.attempts`, duration.Milliseconds()).Scan(&lease.OrderID, &lease.Version, &lease.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return lease, false, nil
	}
	if err != nil {
		return lease, false, fmt.Errorf("claim delivery: %w", err)
	}
	return lease, true, nil
}

func lockLease(ctx context.Context, tx pgx.Tx, lease delivery.Lease) error {
	var paid bool
	var status string
	err := tx.QueryRow(ctx, `SELECT paid_at IS NOT NULL,status FROM orders WHERE id=$1 FOR UPDATE`, lease.OrderID).Scan(&paid, &status)
	if err != nil {
		return err
	}
	if !paid || status == "delivered" || status == "payment_failed" {
		return delivery.ErrLeaseLost
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT state='leased' AND lease_version=$2 AND leased_until>clock_timestamp()
		FROM delivery_jobs WHERE order_id=$1 FOR UPDATE`, lease.OrderID, lease.Version).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return delivery.ErrLeaseLost
	}
	return nil
}

func (s *Store) PrepareDelivery(ctx context.Context, lease delivery.Lease) (delivery.Request, error) {
	var req delivery.Request
	err := transact(ctx, s.pool, func(tx pgx.Tx) error {
		req = delivery.Request{OrderID: lease.OrderID}
		if err := lockLease(ctx, tx, lease); err != nil {
			return err
		}
		var generation int
		var state string
		err := tx.QueryRow(ctx, `SELECT request_id,sku,supplier,generation,state FROM delivery_operations
			WHERE order_id=$1 ORDER BY generation DESC LIMIT 1`, lease.OrderID).Scan(&req.RequestID, &req.SKU, &req.Supplier, &generation, &state)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) || state == "refused" {
			// New IDs are allowed only for a first attempt or after a durable refusal.
			req.RequestID, err = order.NewID("req")
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, "SELECT sku FROM orders WHERE id=$1", lease.OrderID).Scan(&req.SKU); err != nil {
				return err
			}
			if req.Supplier != "A" {
				req.Supplier = "A"
			} else {
				req.Supplier = "B"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_operations(request_id,order_id,supplier,generation,sku)
				VALUES ($1,$2,$3,$4,$5)`, req.RequestID, req.OrderID, req.Supplier, generation+1, req.SKU); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE orders SET status='delivering',updated_at=now() WHERE id=$1`, lease.OrderID)
		return err
	})
	return req, err
}

func (s *Store) FinishDelivery(ctx context.Context, lease delivery.Lease, req delivery.Request, result delivery.Result, delay time.Duration) error {
	if req.OrderID != lease.OrderID || result.RequestID != req.RequestID {
		return order.ErrInvalid
	}
	return transact(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockLease(ctx, tx, lease); err != nil {
			return err
		}
		var active string
		if err := tx.QueryRow(ctx, `SELECT request_id FROM delivery_operations WHERE order_id=$1
			ORDER BY generation DESC LIMIT 1`, lease.OrderID).Scan(&active); err != nil {
			return err
		}
		if active != req.RequestID {
			return delivery.ErrLeaseLost
		}
		if result.Status == "ok" && result.Code != "" {
			if _, err := tx.Exec(ctx, `UPDATE delivery_operations SET state='issued',reason='' WHERE request_id=$1`, req.RequestID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO deliveries(order_id,request_id,supplier,code) VALUES ($1,$2,$3,$4)`, req.OrderID, req.RequestID, req.Supplier, result.Code); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE orders SET status='delivered',updated_at=now() WHERE id=$1`, req.OrderID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE delivery_jobs SET state='done',leased_until=NULL,last_error='' WHERE order_id=$1`, req.OrderID)
			return err
		}
		opState, status, reason := "unknown", "delivery_failed", "supplier_result_unknown"
		if result.Status == "error" && result.Final && (result.Reason == "out_of_stock" || result.Reason == "unavailable") {
			opState, reason = "refused", result.Reason
			if result.Reason == "out_of_stock" {
				status = "out_of_stock"
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_operations SET state=$2,reason=$3 WHERE request_id=$1`, req.RequestID, opState, reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET status=$2,updated_at=now() WHERE id=$1`, req.OrderID, status); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE delivery_jobs SET state='ready',leased_until=NULL,
			available_at=now()+$2::bigint*interval '1 millisecond',last_error=$3 WHERE order_id=$1`, req.OrderID, delay.Milliseconds(), reason)
		return err
	})
}
