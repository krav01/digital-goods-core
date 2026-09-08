package main

import (
	"context"
	"log/slog"

	"github.com/krav01/digital-goods-core/internal/config"
	"github.com/krav01/digital-goods-core/internal/postgres"
	"github.com/krav01/digital-goods-core/internal/process"
	"github.com/krav01/digital-goods-core/internal/supplier"
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
	options := []supplier.HandlerOption{
		supplier.WithAfterIssueDelay(cfg.SupplierAfterIssueDelay),
		supplier.WithRandomFinalUnavailable(cfg.SupplierFinalUnavailableRate),
	}
	if cfg.SupplierForceFinalUnavailable {
		options = append(options, supplier.WithForcedFinalUnavailable())
	}
	return process.Serve(ctx, cfg, supplier.NewHandler(postgres.NewSupplier(pool), options...), logger)
}
