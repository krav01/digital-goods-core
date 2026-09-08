package main

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"github.com/krav01/digital-goods-core/internal/config"
	"github.com/krav01/digital-goods-core/internal/delivery"
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
	u, err := url.Parse(cfg.SupplierURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid SUPPLIER_URL")
	}
	u, err = url.Parse(cfg.SupplierBURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid SUPPLIER_B_URL")
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Ready(ctx, pool); err != nil {
		return err
	}
	issuers := map[string]delivery.Issuer{"A": supplier.NewClient(cfg.SupplierURL), "B": supplier.NewClient(cfg.SupplierBURL)}
	return delivery.NewWorkerWithSuppliers(postgres.New(pool), issuers, logger).Run(ctx)
}
