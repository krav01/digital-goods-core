package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/krav01/digital-goods-core/internal/catalog"
	"github.com/krav01/digital-goods-core/internal/httpjson"
	"github.com/krav01/digital-goods-core/internal/order"
	"github.com/krav01/digital-goods-core/internal/payment"
)

type statusResponse struct {
	Status string `json:"status"`
}

type Store interface {
	Ready(context.Context) error
	CreateOrder(context.Context, string, string) (order.Order, bool, error)
	GetOrder(context.Context, string) (order.Order, error)
	AcceptPayment(context.Context, payment.Event) error
	ListProducts(context.Context) ([]catalog.Product, error)
}

func NewHandler(store Store, inventorySources ...catalog.InventorySource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", statusHandler)
	mux.HandleFunc("GET /products", func(w http.ResponseWriter, r *http.Request) {
		products, err := store.ListProducts(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		if len(inventorySources) == 0 {
			httpjson.Error(w, 503, "inventory unavailable")
			return
		}
		available, err := catalog.Available(r.Context(), inventorySources...)
		if err != nil {
			httpjson.Error(w, 503, "inventory unavailable")
			return
		}
		for i := range products {
			products[i].Available = available[products[i].SKU]
		}
		httpjson.Write(w, 200, products)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if store == nil || store.Ready(r.Context()) != nil {
			httpjson.Error(w, 503, "database not ready")
			return
		}
		statusHandler(w, r)
	})
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			SKU string `json:"sku"`
			ID  string `json:"order_id"`
		}
		if err := httpjson.Decode(w, r, &input); err != nil {
			httpjson.Error(w, 400, "invalid JSON request")
			return
		}
		if input.ID == "" {
			id, err := order.NewID("ord")
			if err != nil {
				respondError(w, err)
				return
			}
			input.ID = id
		}
		if !order.ValidID(input.ID) || !order.ValidID(input.SKU) {
			respondError(w, order.ErrInvalid)
			return
		}
		o, created, err := store.CreateOrder(r.Context(), input.ID, input.SKU)
		if err != nil {
			respondError(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpjson.Write(w, status, o)
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !order.ValidID(id) {
			respondError(w, order.ErrInvalid)
			return
		}
		o, err := store.GetOrder(r.Context(), id)
		if err != nil {
			respondError(w, err)
			return
		}
		httpjson.Write(w, 200, o)
	})
	mux.HandleFunc("POST /webhooks/payment", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID        string      `json:"event_id"`
			OrderID   string      `json:"order_id"`
			Status    string      `json:"status"`
			Amount    json.Number `json:"amount"`
			Currency  string      `json:"currency"`
			CreatedAt time.Time   `json:"created_at"`
		}
		if err := httpjson.Decode(w, r, &input); err != nil {
			httpjson.Error(w, 400, "invalid JSON request")
			return
		}
		amount, err := payment.ParseAmount(input.Amount.String())
		if err != nil {
			respondError(w, err)
			return
		}
		e := payment.Event{ID: input.ID, OrderID: input.OrderID, Status: input.Status, AmountMinor: amount, Currency: input.Currency, CreatedAt: input.CreatedAt}
		if err := e.Validate(); err != nil {
			respondError(w, err)
			return
		}
		if err := store.AcceptPayment(r.Context(), e); err != nil {
			respondError(w, err)
			return
		}
		httpjson.Write(w, 200, statusResponse{Status: "accepted"})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func statusHandler(w http.ResponseWriter, _ *http.Request) {
	httpjson.Write(w, http.StatusOK, statusResponse{Status: "ok"})
}

func respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, order.ErrInvalid):
		httpjson.Error(w, 400, "invalid input")
	case errors.Is(err, order.ErrNotFound):
		httpjson.Error(w, 404, "order or product not found")
	case errors.Is(err, order.ErrConflict):
		httpjson.Error(w, 409, "identifier conflict")
	default:
		slog.Error("API operation failed", "error", err)
		httpjson.Error(w, 503, "temporarily unavailable")
	}
}
