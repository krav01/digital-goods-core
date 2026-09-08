package main

import (
	"context"
	"log/slog"

	"github.com/krav01/digital-goods-core/internal/config"
	"github.com/krav01/digital-goods-core/internal/postgres"
	"github.com/krav01/digital-goods-core/internal/process"
)

func main() { process.Main(run) }

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	report, err := postgres.New(pool).Reconcile(ctx)
	if err != nil {
		return err
	}
	logger.Info("reconciliation report", "pending_payment_events", report.PendingPaymentEvents,
		"payment_conflicts", report.PaymentConflicts, "paid_without_delivery", report.PaidWithoutDelivery,
		"delivery_without_paid", report.DeliveryWithoutPaid, "expired_leases", report.ExpiredLeases,
		"unknown_operations", report.UnknownOperations, "has_anomalies", report.HasAnomalies())
	return nil
}
