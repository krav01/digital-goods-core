package main

import (
	"context"
	"log/slog"

	"github.com/krav01/digital-goods-core/internal/config"
	"github.com/krav01/digital-goods-core/internal/httpapi"
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
	return process.Serve(ctx, cfg, httpapi.NewHandler(postgres.New(pool), supplier.NewClient(cfg.SupplierURL), supplier.NewClient(cfg.SupplierBURL)), logger)
}
