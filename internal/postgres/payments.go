package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/payment"
)

func (s *Store) AcceptPayment(ctx context.Context, e payment.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	e.CreatedAt = e.CreatedAt.UTC().Truncate(time.Microsecond)
	return transact(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO payment_events(event_id,order_id,status,amount_minor,currency,created_at)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (event_id) DO NOTHING`, e.ID, e.OrderID, e.Status, e.AmountMinor, e.Currency, e.CreatedAt)
		if err != nil {
			return fmt.Errorf("save payment event: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		var equal bool
		err = tx.QueryRow(ctx, `SELECT order_id=$2 AND status=$3 AND amount_minor=$4 AND currency=$5 AND created_at=$6
			FROM payment_events WHERE event_id=$1`, e.ID, e.OrderID, e.Status, e.AmountMinor, e.Currency, e.CreatedAt).Scan(&equal)
		if err != nil {
			return fmt.Errorf("compare payment replay: %w", err)
		}
		if !equal {
			return order.ErrConflict
		}
		return nil
	})
}

func (s *Store) ProcessPayment(ctx context.Context) (bool, error) {
	worked := false
	err := transact(ctx, s.pool, func(tx pgx.Tx) error {
		worked = false
		var e payment.Event
		err := tx.QueryRow(ctx, `SELECT event_id,order_id,status,amount_minor,currency,created_at FROM payment_events
			WHERE processing_state IN ('pending','waiting_order') AND next_attempt_at<=now()
			ORDER BY next_attempt_at,received_at,event_id LIMIT 1 FOR UPDATE SKIP LOCKED`).
			Scan(&e.ID, &e.OrderID, &e.Status, &e.AmountMinor, &e.Currency, &e.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim payment event: %w", err)
		}
		worked = true
		var o order.Order
		err = tx.QueryRow(ctx, `SELECT id,price_minor,currency,status,paid_at FROM orders WHERE id=$1 FOR UPDATE`, e.OrderID).
			Scan(&o.ID, &o.PriceMinor, &o.Currency, &o.Status, &o.PaidAt)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx, `UPDATE payment_events SET processing_state='waiting_order',next_attempt_at=now()+interval '1 second'
				WHERE event_id=$1`, e.ID)
			return err
		}
		if err != nil {
			return fmt.Errorf("lock payment order: %w", err)
		}
		state, reason := payment.Decide(o, e)
		if state == "applied" && reason == "paid" {
			if _, err := tx.Exec(ctx, `UPDATE orders SET status='paid',paid_at=now(),updated_at=now() WHERE id=$1`, o.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_jobs(order_id) VALUES ($1) ON CONFLICT DO NOTHING`, o.ID); err != nil {
				return err
			}
		}
		if state == "applied" && reason == "failed" && o.Status == "created" {
			if _, err := tx.Exec(ctx, `UPDATE orders SET status='payment_failed',updated_at=now() WHERE id=$1`, o.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE payment_events SET processing_state=$2,error=$3 WHERE event_id=$1`, e.ID, state, reason)
		return err
	})
	return worked, err
}
