package delivery

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		intn    func(int) int
		want    time.Duration
	}{
		{"first attempt minimum jitter", 1, func(int) int { return 0 }, 500 * time.Millisecond},
		{"first attempt maximum jitter", 1, func(n int) int { return n - 1 }, time.Second},
		{"sixth attempt capped minimum jitter", 6, func(int) int { return 0 }, 16 * time.Second},
		{"later attempt stays capped", 20, func(n int) int { return n - 1 }, 32 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryDelay(test.attempt, test.intn); got != test.want {
				t.Fatalf("retryDelay(%d) = %s, want %s", test.attempt, got, test.want)
			}
		})
	}
}
