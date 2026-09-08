package postgres

import (
	"context"
	"fmt"

	"github.com/krav01/digital-goods-core/internal/reconcile"
)

// Reconcile returns consistency signals without changing application state.
func (s *Store) Reconcile(ctx context.Context) (reconcile.Report, error) {
	var report reconcile.Report
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM payment_events WHERE processing_state IN ('pending','waiting_order')),
		(SELECT count(*) FROM payment_events WHERE processing_state='conflict'),
		(SELECT count(*) FROM orders o WHERE o.paid_at IS NOT NULL AND o.status <> 'delivered'
			AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.order_id=o.id)),
		(SELECT count(*) FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE o.paid_at IS NULL),
		(SELECT count(*) FROM delivery_jobs WHERE state='leased' AND leased_until < clock_timestamp()),
		(SELECT count(*) FROM delivery_operations WHERE state='unknown')`).Scan(
		&report.PendingPaymentEvents, &report.PaymentConflicts, &report.PaidWithoutDelivery,
		&report.DeliveryWithoutPaid, &report.ExpiredLeases, &report.UnknownOperations,
	)
	if err != nil {
		return reconcile.Report{}, fmt.Errorf("reconcile application database: %w", err)
	}
	return report, nil
}
