package admin

import (
	"errors"
	"regexp"
	"time"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidSlug       = errors.New("admin: invalid slug")
	ErrInvalidStatus     = errors.New("admin: invalid status")
	ErrInvalidPrice      = errors.New("admin: invalid price")
	ErrInvalidCurrency   = errors.New("admin: invalid currency")
	ErrDuplicateSKU      = errors.New("admin: duplicate SKU")
	ErrProductInUse      = errors.New("admin: product in use")
	ErrProductNotFound   = errors.New("admin: product not found")
	ErrInsufficientStock = errors.New("admin: insufficient stock")
	ErrInvalidOnHand     = errors.New("admin: invalid on_hand value")
	ErrInvalidLocationID = errors.New("admin: invalid location_id")
	ErrDuplicateSlug     = errors.New("admin: duplicate slug")
	slugRegex            = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	validCurrencies      = map[string]bool{
		"USD": true, "KES": true, "EUR": true, "GBP": true, "NGN": true,
		"ZAR": true, "GHS": true, "TZS": true, "UGX": true, "RWF": true,
	}
)

type ProductInput struct {
	Slug     string         `json:"slug"`
	Name     string         `json:"name"`
	Status   string         `json:"status"`
	Variants []VariantInput `json:"variants"`
}

type VariantInput struct {
	SKU      string `json:"sku"`
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
}

type StockUpdate struct {
	LocationID string `json:"location_id"`
	OnHand     int    `json:"on_hand"`
}

type ValidationError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *ValidationError) Error() string {
	return e.Message
}

func ValidateSlug(slug string) *ValidationError {
	if len(slug) == 0 || len(slug) > 80 {
		return &ValidationError{
			Code:    "invalid_slug",
			Message: "slug must be between 1 and 80 characters",
			Fields:  map[string]string{"slug": "must be between 1 and 80 characters"},
		}
	}
	if !slugRegex.MatchString(slug) {
		return &ValidationError{
			Code:    "invalid_slug",
			Message: "slug must contain only lowercase letters, numbers, and hyphens",
			Fields:  map[string]string{"slug": "must contain only lowercase letters, numbers, and hyphens"},
		}
	}
	return nil
}

func ValidateStatus(status string) *ValidationError {
	if status == "" {
		return nil
	}
	validStatuses := map[string]bool{"draft": true, "published": true, "archived": true}
	if !validStatuses[status] {
		return &ValidationError{
			Code:    "invalid_status",
			Message: "status must be draft, published, or archived",
			Fields:  map[string]string{"status": "must be draft, published, or archived"},
		}
	}
	return nil
}

func ValidatePrice(price int64) *ValidationError {
	if price < 0 {
		return &ValidationError{
			Code:    "invalid_price",
			Message: "price must be non-negative",
			Fields:  map[string]string{"price": "must be non-negative"},
		}
	}
	return nil
}

func ValidateCurrency(currency string) *ValidationError {
	if currency == "" {
		return &ValidationError{
			Code:    "invalid_currency",
			Message: "currency is required",
			Fields:  map[string]string{"currency": "is required"},
		}
	}
	if !validCurrencies[currency] {
		return &ValidationError{
			Code:    "invalid_currency",
			Message: "unknown currency code",
			Fields:  map[string]string{"currency": "unknown currency code"},
		}
	}
	return nil
}

func ValidateProductInput(input ProductInput) *ValidationError {
	if input.Name == "" {
		return &ValidationError{
			Code:    "invalid_name",
			Message: "name is required",
			Fields:  map[string]string{"name": "is required"},
		}
	}
	if err := ValidateSlug(input.Slug); err != nil {
		return err
	}
	if err := ValidateStatus(input.Status); err != nil {
		return err
	}
	skus := make(map[string]bool)
	for i, v := range input.Variants {
		if v.SKU == "" {
			return &ValidationError{
				Code:    "invalid_sku",
				Message: "variant SKU is required",
				Fields:  map[string]string{"variants[" + string(rune('0'+i)) + "].sku": "is required"},
			}
		}
		if skus[v.SKU] {
			return &ValidationError{
				Code:    "duplicate_sku",
				Message: "duplicate SKU found: " + v.SKU,
				Fields:  map[string]string{"variants[" + string(rune('0'+i)) + "].sku": "duplicate"},
			}
		}
		skus[v.SKU] = true
		if err := ValidatePrice(v.Price); err != nil {
			return &ValidationError{
				Code:    "invalid_price",
				Message: err.Message,
				Fields:  map[string]string{"variants[" + string(rune('0'+i)) + "].price": err.Fields["price"]},
			}
		}
		if err := ValidateCurrency(v.Currency); err != nil {
			return &ValidationError{
				Code:    "invalid_currency",
				Message: err.Message,
				Fields:  map[string]string{"variants[" + string(rune('0'+i)) + "].currency": err.Fields["currency"]},
			}
		}
	}
	return nil
}

func ValidateStockUpdate(input StockUpdate) *ValidationError {
	if input.LocationID == "" {
		return &ValidationError{
			Code:    "invalid_location_id",
			Message: "location_id is required",
			Fields:  map[string]string{"location_id": "is required"},
		}
	}
	if input.OnHand < 0 {
		return &ValidationError{
			Code:    "invalid_on_hand",
			Message: "on_hand must be non-negative",
			Fields:  map[string]string{"on_hand": "must be non-negative"},
		}
	}
	return nil
}

func ToProduct(input ProductInput, id string) *catalog.Product {
	product := &catalog.Product{
		ID:       id,
		Slug:     input.Slug,
		Name:     input.Name,
		Status:   input.Status,
		Variants: make([]catalog.Variant, len(input.Variants)),
	}
	if product.Status == "" {
		product.Status = "draft"
	}
	for i, v := range input.Variants {
		product.Variants[i] = catalog.Variant{
			ID:       generateVariantID(),
			SKU:      v.SKU,
			Price:    money.NewAmount(v.Price, v.Currency),
			Currency: v.Currency,
		}
	}
	return product
}

func generateVariantID() string {
	return "var_" + time.Now().Format("20060102150405") + randomSuffix(4)
}

func randomSuffix(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[time.Now().Nanosecond()%len(charset)]
	}
	return string(b)
}
