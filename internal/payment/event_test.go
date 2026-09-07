package payment

import (
	"testing"
	"time"

	"github.com/krav01/digital-goods-core/internal/order"
)

func TestParseAmount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw   string
		want  int64
		valid bool
	}{
		{"500", 50000, true}, {"500.00", 50000, true}, {"0.01", 1, true}, {"1.2", 120, true},
		{"92233720368547758.07", 9223372036854775807, true},
		{"92233720368547758.08", 0, false}, {"500.001", 0, false}, {"1e2", 0, false},
		{"-1", 0, false}, {"+1", 0, false}, {"0", 0, false}, {"", 0, false}, {"null", 0, false},
		{"1.", 0, false}, {".01", 0, false}, {"1.2.3", 0, false}, {" 1", 0, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAmount(tc.raw)
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatalf("ParseAmount(%q) = %d,%v; want %d valid=%v", tc.raw, got, err, tc.want, tc.valid)
			}
		})
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, tc := range []struct {
		name, status, event    string
		paid                   bool
		amount                 int64
		currency, want, reason string
	}{
		{"paid", "created", "paid", false, 50000, "RUB", "applied", "paid"},
		{"failed", "created", "failed", false, 50000, "RUB", "applied", "failed"},
		{"replay", "delivered", "paid", true, 50000, "RUB", "applied", "duplicate_payment"},
		{"late_failure", "delivered", "failed", true, 50000, "RUB", "conflict", "failure_after_payment"},
		{"late_success", "payment_failed", "paid", false, 50000, "RUB", "conflict", "paid_after_failure"},
		{"wrong_amount", "created", "paid", false, 49900, "RUB", "rejected", "amount_or_currency_mismatch"},
		{"wrong_currency", "created", "paid", false, 50000, "USD", "rejected", "amount_or_currency_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := order.Order{PriceMinor: 50000, Currency: "RUB", Status: tc.status}
			if tc.paid {
				o.PaidAt = &now
			}
			got, reason := Decide(o, Event{Status: tc.event, AmountMinor: tc.amount, Currency: tc.currency})
			if got != tc.want || reason != tc.reason {
				t.Fatalf("got %s/%s, want %s/%s", got, reason, tc.want, tc.reason)
			}
		})
	}
}
