package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrValidation             = errors.New("admin: validation error")
	ErrInvalidSlug            = errors.New("admin: invalid slug")
	ErrInvalidStatus          = errors.New("admin: invalid status")
	ErrInvalidPrice           = errors.New("admin: invalid price")
	ErrInvalidCurrency        = errors.New("admin: invalid currency")
	ErrDuplicateSKU           = errors.New("admin: duplicate SKU")
	ErrProductNotFound        = errors.New("admin: product not found")
	ErrProductInUse           = errors.New("admin: product in use")
	ErrInsufficientStock      = errors.New("admin: insufficient stock")
	ErrInvalidLimit           = errors.New("admin: invalid limit")
	ErrInvalidCursor          = errors.New("admin: invalid cursor")
	ErrIdempotencyKeyRequired = errors.New("admin: idempotency key required")

	slugRegex     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	currencyRegex = regexp.MustCompile(`^[A-Z]{3}$`)
)

type ProductStatus string

const (
	StatusDraft     ProductStatus = "draft"
	StatusPublished ProductStatus = "published"
	StatusArchived  ProductStatus = "archived"
)

type ProductInput struct {
	Slug     string         `json:"slug"`
	Name     string         `json:"name"`
	Status   ProductStatus  `json:"status"`
	Variants []VariantInput `json:"variants"`
}

type VariantInput struct {
	SKU      string `json:"sku"`
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
}

type Product struct {
	ID        string        `json:"id"`
	Slug      string        `json:"slug"`
	Name      string        `json:"name"`
	Status    ProductStatus `json:"status"`
	Variants  []Variant     `json:"variants"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type Variant struct {
	ID        string       `json:"id"`
	ProductID string       `json:"product_id"`
	SKU       string       `json:"sku"`
	Price     money.Amount `json:"price"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type StockLevel struct {
	VariantID  string `json:"variant_id"`
	LocationID string `json:"location_id"`
	OnHand     int    `json:"on_hand"`
	Reserved   int    `json:"reserved"`
}

func (s StockLevel) Available() int {
	return s.OnHand - s.Reserved
}

type StockUpdate struct {
	VariantID  string `json:"variant_id"`
	LocationID string `json:"location_id"`
	OnHand     int    `json:"on_hand"`
}

type ValidationError struct {
	Message string
	Fields  map[string]string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func ValidateProductInput(input ProductInput) *ValidationError {
	fields := make(map[string]string)

	if err := ValidateSlug(input.Slug); err != nil {
		fields["slug"] = err.Error()
	}

	if input.Name == "" {
		fields["name"] = "name cannot be empty"
	}

	if input.Status != "" && input.Status != StatusDraft && input.Status != StatusPublished {
		fields["status"] = "status must be draft or published"
	}

	skus := make(map[string]bool)
	for i, v := range input.Variants {
		prefix := fmt.Sprintf("variants[%d]", i)

		if v.SKU == "" {
			fields[prefix+".sku"] = "SKU cannot be empty"
		} else if skus[v.SKU] {
			fields[prefix+".sku"] = "duplicate SKU"
		} else {
			skus[v.SKU] = true
		}

		if v.Price < 0 {
			fields[prefix+".price"] = "price cannot be negative"
		}

		if !currencyRegex.MatchString(v.Currency) {
			fields[prefix+".currency"] = "currency must be a valid ISO-4217 code"
		}
	}

	if len(fields) > 0 {
		return &ValidationError{
			Message: "validation failed",
			Fields:  fields,
		}
	}

	return nil
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

type ProductListOptions struct {
	Status ProductStatus
	Limit  int
	Cursor string
}

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

type ProductRepository interface {
	Create(ctx context.Context, input ProductInput, idempotencyKey string) (*Product, error)
	Get(ctx context.Context, id string) (*Product, error)
	GetBySlug(ctx context.Context, slug string) (*Product, error)
	List(ctx context.Context, opts ProductListOptions) ([]Product, string, error)
	Update(ctx context.Context, id string, input ProductInput) (*Product, error)
	Delete(ctx context.Context, id string) error
	HasOrders(ctx context.Context, productID string) (bool, error)
}

type StockRepository interface {
	GetLevel(ctx context.Context, variantID, locationID string) (*StockLevel, error)
	SetLevel(ctx context.Context, update StockUpdate) (*StockLevel, error)
	UpdateLevel(ctx context.Context, variantID, locationID string, delta int) (*StockLevel, error)
}
