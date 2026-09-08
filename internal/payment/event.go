package payment

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/krav01/digital-goods-core/internal/order"
)

type Event struct {
	ID          string
	OrderID     string
	Status      string
	AmountMinor int64
	Currency    string
	CreatedAt   time.Time
}

func (e Event) Validate() error {
	if !order.ValidID(e.ID) || !order.ValidID(e.OrderID) ||
		(e.Status != "paid" && e.Status != "failed") || e.AmountMinor <= 0 ||
		len(e.Currency) != 3 || e.CreatedAt.IsZero() || e.CreatedAt.Year() < 1 || e.CreatedAt.Year() > 9999 {
		return order.ErrInvalid
	}
	for _, c := range e.Currency {
		if c < 'A' || c > 'Z' {
			return order.ErrInvalid
		}
	}
	return nil
}

// ParseAmount converts a plain decimal RUB amount into kopecks without floating point.
// Scientific notation, signs and sub-kopeck fractions are deliberately rejected.
func ParseAmount(raw string) (int64, error) {
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 17 {
		return 0, order.ErrInvalid
	}
	for _, part := range parts {
		if part == "" {
			return 0, order.ErrInvalid
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, order.ErrInvalid
			}
		}
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, order.ErrInvalid
		}
		fraction = parts[1] + strings.Repeat("0", 2-len(parts[1]))
	}
	minor, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	if err != nil || minor <= 0 {
		return 0, fmt.Errorf("amount: %w", order.ErrInvalid)
	}
	return minor, nil
}

// Decide preserves terminal states; conflicting payment notifications require reconciliation.
func Decide(o order.Order, e Event) (state, reason string) {
	if e.AmountMinor != o.PriceMinor || e.Currency != o.Currency {
		return "rejected", "amount_or_currency_mismatch"
	}
	if e.Status == "paid" {
		if o.Status == "payment_failed" {
			return "conflict", "paid_after_failure"
		}
		if o.PaidAt != nil {
			return "applied", "duplicate_payment"
		}
		return "applied", "paid"
	}
	if o.PaidAt != nil {
		return "conflict", "failure_after_payment"
	}
	return "applied", "failed"
}
