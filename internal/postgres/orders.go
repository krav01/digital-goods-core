package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krav01/digital-goods-core/internal/order"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store              { return &Store{pool: pool} }
func (s *Store) Ready(ctx context.Context) error { return Ready(ctx, s.pool) }

func (s *Store) CreateOrder(ctx context.Context, id, sku string) (order.Order, bool, error) {
	if !order.ValidID(id) || !order.ValidID(sku) {
		return order.Order{}, false, order.ErrInvalid
	}
	created := false
	err := transact(ctx, s.pool, func(tx pgx.Tx) error {
		created = false
		existing, err := readOrder(ctx, tx, id)
		if err == nil {
			if existing.SKU != sku {
				return order.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, order.ErrNotFound) {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO orders(id,sku,price_minor,currency)
			SELECT $1,sku,price_minor,currency FROM products WHERE sku=$2 AND active
			ON CONFLICT (id) DO NOTHING`, id, sku)
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		created = tag.RowsAffected() == 1
		got, err := readOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		if got.SKU != sku {
			return order.ErrConflict
		}
		return nil
	})
	if err != nil {
		return order.Order{}, false, err
	}
	got, err := s.GetOrder(ctx, id)
	return got, created, err
}

func (s *Store) GetOrder(ctx context.Context, id string) (order.Order, error) {
	return readOrder(ctx, s.pool, id)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readOrder(ctx context.Context, db rowQuerier, id string) (order.Order, error) {
	var o order.Order
	err := db.QueryRow(ctx, `SELECT o.id,o.sku,o.price_minor,o.currency,o.status,o.paid_at,o.created_at,
		CASE WHEN o.status='delivered' THEN COALESCE(d.code,'') ELSE '' END
		FROM orders o LEFT JOIN deliveries d ON d.order_id=o.id WHERE o.id=$1`, id).
		Scan(&o.ID, &o.SKU, &o.PriceMinor, &o.Currency, &o.Status, &o.PaidAt, &o.CreatedAt, &o.Code)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, order.ErrNotFound
	}
	if err != nil {
		return o, fmt.Errorf("read order: %w", err)
	}
	return o, nil
}
