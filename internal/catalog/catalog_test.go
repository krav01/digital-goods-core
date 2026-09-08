package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/krav01/digital-goods-core/internal/supplier"
)

type sourceFunc func(context.Context) ([]supplier.Inventory, error)

func (f sourceFunc) Inventory(ctx context.Context) ([]supplier.Inventory, error) { return f(ctx) }

func TestAvailable(t *testing.T) {
	good := sourceFunc(func(context.Context) ([]supplier.Inventory, error) {
		return []supplier.Inventory{{SKU: "A", Available: 2}}, nil
	})
	second := sourceFunc(func(context.Context) ([]supplier.Inventory, error) {
		return []supplier.Inventory{{SKU: "A", Available: 3}, {SKU: "B", Available: 1}}, nil
	})
	got, err := Available(t.Context(), good, second)
	if err != nil || got["A"] != 5 || got["B"] != 1 {
		t.Fatalf("available = %#v, %v", got, err)
	}
	bad := sourceFunc(func(context.Context) ([]supplier.Inventory, error) { return nil, errors.New("down") })
	if got, err := Available(t.Context(), good, bad); err == nil || got != nil {
		t.Fatalf("available = %#v, %v", got, err)
	}
}
