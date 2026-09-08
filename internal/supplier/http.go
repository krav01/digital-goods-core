package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/httpjson"
	"github.com/krav01/digital-goods-core/internal/order"
)

type Store interface {
	Issue(context.Context, delivery.Request) (delivery.Result, error)
	Refuse(context.Context, delivery.Request) (delivery.Result, error)
	Inventory(context.Context) ([]Inventory, error)
	Ready(context.Context) error
}

type Inventory struct {
	SKU       string `json:"sku"`
	Available int64  `json:"available"`
}

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	afterIssueDelay       time.Duration
	forceFinalUnavailable bool
	randomUnavailableRate int
}

func WithAfterIssueDelay(delay time.Duration) HandlerOption {
	return func(options *handlerOptions) { options.afterIssueDelay = delay }
}

func WithForcedFinalUnavailable() HandlerOption {
	return func(options *handlerOptions) { options.forceFinalUnavailable = true }
}

func WithRandomFinalUnavailable(rate int) HandlerOption {
	return func(options *handlerOptions) { options.randomUnavailableRate = rate }
}

func NewHandler(store Store, options ...HandlerOption) http.Handler {
	config := handlerOptions{}
	for _, option := range options {
		option(&config)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ready(r.Context()); err != nil {
			httpjson.Error(w, 503, "database not ready")
			return
		}
		httpjson.Write(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /inventory", func(w http.ResponseWriter, r *http.Request) {
		inventory, err := store.Inventory(r.Context())
		if err != nil {
			slog.Error("read supplier inventory", "error", err)
			httpjson.Error(w, 503, "temporarily unavailable")
			return
		}
		httpjson.Write(w, 200, inventory)
	})
	mux.HandleFunc("POST /issue", func(w http.ResponseWriter, r *http.Request) {
		var req delivery.Request
		if err := httpjson.Decode(w, r, &req); err != nil {
			httpjson.Error(w, 400, "invalid request")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var result delivery.Result
		var err error
		if config.forceFinalUnavailable || shouldRefuseRandomly(config.randomUnavailableRate) {
			result, err = store.Refuse(ctx, req)
		} else {
			result, err = store.Issue(ctx, req)
		}
		if errors.Is(err, order.ErrInvalid) {
			httpjson.Error(w, 400, "invalid input")
			return
		}
		if errors.Is(err, order.ErrConflict) {
			httpjson.Error(w, 409, "identifier conflict")
			return
		}
		if err != nil {
			slog.Error("supplier operation failed", "request_id", req.RequestID, "error", err)
			httpjson.Error(w, 503, "temporarily unavailable")
			return
		}
		if config.afterIssueDelay > 0 {
			select {
			case <-time.After(config.afterIssueDelay):
			case <-r.Context().Done():
				return
			}
		}
		status := 200
		if result.Status == "error" {
			status = 409
		}
		httpjson.Write(w, status, result)
	})
	return mux
}

func shouldRefuseRandomly(rate int) bool {
	return rate > 0 && rand.IntN(100) < rate
}

type Client struct {
	url    string
	client *http.Client
}

func NewClient(url string) *Client {
	return &Client{url: strings.TrimRight(url, "/") + "/issue", client: &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) Issue(ctx context.Context, input delivery.Request) (delivery.Result, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return delivery.Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return delivery.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return delivery.Result{}, fmt.Errorf("supplier request: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Warn("close supplier response", "request_id", input.RequestID, "error", err)
		}
	}()
	var result delivery.Result
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<10))
	if err := decoder.Decode(&result); err != nil {
		return result, errors.New("invalid supplier response")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return result, errors.New("trailing supplier response data")
	}
	if result.RequestID != input.RequestID {
		return result, errors.New("supplier request identifier mismatch")
	}
	if response.StatusCode == 200 && result.Status == "ok" && result.Code != "" && len(result.Code) <= 512 {
		return result, nil
	}
	if response.StatusCode >= 400 && response.StatusCode <= 599 && result.Status == "error" && result.Final &&
		(result.Reason == "out_of_stock" || result.Reason == "unavailable") && result.Code == "" {
		return result, nil
	}
	return result, errors.New("supplier result is not definitive")
}
