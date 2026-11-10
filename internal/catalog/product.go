package catalog

import (
	"errors"
	"regexp"
	"strings"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidSlug     = errors.New("invalid slug")
	ErrProductNotFound = errors.New("product not found")
	ErrInvalidLimit    = errors.New("invalid limit")
	ErrInvalidCursor   = errors.New("invalid cursor")
	slugRegex          = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type Product struct {
	ID       string    `json:"id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Variants []Variant `json:"variants"`
}

type Variant struct {
	ID       string       `json:"id"`
	SKU      string       `json:"sku"`
	Price    money.Amount `json:"price"`
	Currency string       `json:"currency"`
}

func ValidateSlug(slug string) error {
	if len(slug) == 0 || len(slug) > 80 {
		return ErrInvalidSlug
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return ErrInvalidSlug
	}
	if !slugRegex.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

type ListOptions struct {
	Limit  int
	Cursor string
}

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

type Repository interface {
	Get(ctx interface{}, id string) (*Product, error)
	List(ctx interface{}, opts ListOptions) ([]Product, string, error)
}
