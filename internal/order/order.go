package order

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("identifier already used with different fields")
	ErrInvalid  = errors.New("invalid input")
	identifier  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
)

type Order struct {
	ID         string     `json:"order_id"`
	SKU        string     `json:"sku"`
	PriceMinor int64      `json:"price_minor"`
	Currency   string     `json:"currency"`
	Status     string     `json:"status"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Code       string     `json:"code,omitempty"`
}

func ValidID(value string) bool { return identifier.MatchString(value) }

func NewID(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(data[:]), nil
}
