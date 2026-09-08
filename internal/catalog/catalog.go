package catalog

import (
	"context"
	"fmt"

	"github.com/krav01/digital-goods-core/internal/supplier"
)

type InventorySource interface {
	Inventory(context.Context) ([]supplier.Inventory, error)
}

type Product struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	PriceMinor int64  `json:"price_minor"`
	Currency   string `json:"currency"`
	Available  int64  `json:"available"`
}

func Available(ctx context.Context, sources ...InventorySource) (map[string]int64, error) {
	available := make(map[string]int64)
	for _, source := range sources {
		inventory, err := source.Inventory(ctx)
		if err != nil {
			return nil, fmt.Errorf("read inventory: %w", err)
		}
		for _, item := range inventory {
			available[item.SKU] += item.Available
		}
	}
	return available, nil
}
