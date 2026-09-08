package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/supplier"
)

type SupplierStore struct{ pool *pgxpool.Pool }

func NewSupplier(pool *pgxpool.Pool) *SupplierStore      { return &SupplierStore{pool: pool} }
func (s *SupplierStore) Ready(ctx context.Context) error { return Ready(ctx, s.pool) }

func (s *SupplierStore) Issue(ctx context.Context, req delivery.Request) (delivery.Result, error) {
	return s.issue(ctx, req, false)
}

func (s *SupplierStore) Refuse(ctx context.Context, req delivery.Request) (delivery.Result, error) {
	return s.issue(ctx, req, true)
}

func (s *SupplierStore) Inventory(ctx context.Context) ([]supplier.Inventory, error) {
	rows, err := s.pool.Query(ctx, `SELECT sku,count(*) FROM inventory_keys
		WHERE issued_request_id IS NULL GROUP BY sku ORDER BY sku`)
	if err != nil {
		return nil, fmt.Errorf("query supplier inventory: %w", err)
	}
	defer rows.Close()
	var inventory []supplier.Inventory
	for rows.Next() {
		var item supplier.Inventory
		if err := rows.Scan(&item.SKU, &item.Available); err != nil {
			return nil, err
		}
		inventory = append(inventory, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supplier inventory: %w", err)
	}
	return inventory, nil
}

func (s *SupplierStore) issue(ctx context.Context, req delivery.Request, forceUnavailable bool) (delivery.Result, error) {
	if !order.ValidID(req.RequestID) || !order.ValidID(req.OrderID) || !order.ValidID(req.SKU) {
		return delivery.Result{}, order.ErrInvalid
	}
	var result delivery.Result
	err := transact(ctx, s.pool, func(tx pgx.Tx) error {
		result = delivery.Result{RequestID: req.RequestID}
		// Serialize by order even across different request IDs. Hash collisions only serialize unrelated orders.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, req.OrderID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO issue_requests(request_id,order_id,sku,outcome)
			VALUES ($1,$2,$3,'pending') ON CONFLICT(request_id) DO NOTHING`, req.RequestID, req.OrderID, req.SKU)
		if err != nil {
			return err
		}
		var storedOrder, sku, outcome string
		if err := tx.QueryRow(ctx, `SELECT order_id,sku,outcome,COALESCE(code,''),reason FROM issue_requests
			WHERE request_id=$1 FOR UPDATE`, req.RequestID).Scan(&storedOrder, &sku, &outcome, &result.Code, &result.Reason); err != nil {
			return err
		}
		if storedOrder != req.OrderID || sku != req.SKU {
			return order.ErrConflict
		}
		if tag.RowsAffected() == 0 {
			if outcome == "issued" {
				result.Status = "ok"
			} else {
				result.Status, result.Final = "error", true
			}
			return nil
		}
		var alreadyIssued bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM issue_requests WHERE order_id=$1 AND outcome='issued')`, req.OrderID).Scan(&alreadyIssued); err != nil {
			return err
		}
		if alreadyIssued {
			return order.ErrConflict
		}
		if forceUnavailable {
			result.Status, result.Final, result.Reason = "error", true, "unavailable"
			_, err = tx.Exec(ctx, `UPDATE issue_requests SET outcome='refused',reason='unavailable' WHERE request_id=$1`, req.RequestID)
			return err
		}
		err = tx.QueryRow(ctx, `SELECT code FROM inventory_keys WHERE sku=$1 AND issued_request_id IS NULL
			ORDER BY code LIMIT 1 FOR UPDATE SKIP LOCKED`, req.SKU).Scan(&result.Code)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status, result.Final, result.Reason = "error", true, "out_of_stock"
			_, err = tx.Exec(ctx, `UPDATE issue_requests SET outcome='refused',reason='out_of_stock' WHERE request_id=$1`, req.RequestID)
			return err
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_keys SET issued_request_id=$1 WHERE code=$2`, req.RequestID, result.Code); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE issue_requests SET outcome='issued',code=$2 WHERE request_id=$1`, req.RequestID, result.Code); err != nil {
			return err
		}
		result.Status = "ok"
		return nil
	})
	return result, err
}
