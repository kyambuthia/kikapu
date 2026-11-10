package pricing

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestChannel_IsCurrencyAllowed(t *testing.T) {
	channel := &Channel{
		ID:                "ch1",
		Name:              "Web Store",
		AllowedCurrencies: []string{"USD", "EUR", "GBP"},
	}

	tests := []struct {
		name     string
		currency string
		want     bool
	}{
		{"allowed USD", "USD", true},
		{"allowed EUR", "EUR", true},
		{"allowed GBP", "GBP", true},
		{"not allowed JPY", "JPY", false},
		{"not allowed CAD", "CAD", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channel.IsCurrencyAllowed(tt.currency); got != tt.want {
				t.Errorf("IsCurrencyAllowed(%q) = %v, want %v", tt.currency, got, tt.want)
			}
		})
	}
}

func TestChannel_IsProductPublished(t *testing.T) {
	channel := &Channel{
		ID:                "ch1",
		Name:              "Web Store",
		PublishedProducts: map[string]bool{"prod1": true, "prod2": true},
	}

	tests := []struct {
		name      string
		productID string
		want      bool
	}{
		{"published prod1", "prod1", true},
		{"published prod2", "prod2", true},
		{"not published prod3", "prod3", false},
		{"not published empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channel.IsProductPublished(tt.productID); got != tt.want {
				t.Errorf("IsProductPublished(%q) = %v, want %v", tt.productID, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	product := &catalog.Product{
		ID:     "prod1",
		Slug:   "test-product",
		Name:   "Test Product",
		Status: "published",
		Variants: []catalog.Variant{
			{ID: "var1", SKU: "SKU1", Price: money.Amount{Value: 1000, Currency: "USD"}, Currency: "USD"},
			{ID: "var2", SKU: "SKU2", Price: money.Amount{Value: 900, Currency: "EUR"}, Currency: "EUR"},
		},
	}

	channel := &Channel{
		ID:                "ch1",
		Name:              "Web Store",
		AllowedCurrencies: []string{"USD", "EUR", "GBP"},
		PublishedProducts: map[string]bool{"prod1": true},
	}

	t.Run("happy path USD", func(t *testing.T) {
		price, err := Resolve(product, channel, "USD")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if price.Value != 1000 {
			t.Errorf("expected price 1000, got %d", price.Value)
		}
		if price.Currency != "USD" {
			t.Errorf("expected currency USD, got %s", price.Currency)
		}
	})

	t.Run("happy path EUR", func(t *testing.T) {
		price, err := Resolve(product, channel, "EUR")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if price.Value != 900 {
			t.Errorf("expected price 900, got %d", price.Value)
		}
		if price.Currency != "EUR" {
			t.Errorf("expected currency EUR, got %s", price.Currency)
		}
	})

	t.Run("unknown currency", func(t *testing.T) {
		_, err := Resolve(product, channel, "JPY")
		if err != ErrUnknownCurrency {
			t.Errorf("expected ErrUnknownCurrency, got %v", err)
		}
	})

	t.Run("product not published", func(t *testing.T) {
		unpublishedProduct := &catalog.Product{
			ID:       "prod2",
			Slug:     "unpublished",
			Name:     "Unpublished Product",
			Status:   "draft",
			Variants: []catalog.Variant{},
		}
		_, err := Resolve(unpublishedProduct, channel, "USD")
		if err != ErrProductNotPublished {
			t.Errorf("expected ErrProductNotPublished, got %v", err)
		}
	})

	t.Run("no variant price for currency", func(t *testing.T) {
		productNoGBP := &catalog.Product{
			ID:     "prod3",
			Slug:   "no-gbp",
			Name:   "No GBP Product",
			Status: "published",
			Variants: []catalog.Variant{
				{ID: "var1", SKU: "SKU1", Price: money.Amount{Value: 1000, Currency: "USD"}, Currency: "USD"},
			},
		}
		channelWithGBP := &Channel{
			ID:                "ch2",
			Name:              "UK Store",
			AllowedCurrencies: []string{"USD", "GBP"},
			PublishedProducts: map[string]bool{"prod3": true},
		}

		_, err := Resolve(productNoGBP, channelWithGBP, "GBP")
		if err != ErrNoVariantPrice {
			t.Errorf("expected ErrNoVariantPrice, got %v", err)
		}
	})
}

func TestResolve_EdgeCases(t *testing.T) {
	t.Run("empty variants", func(t *testing.T) {
		product := &catalog.Product{
			ID:       "prod1",
			Slug:     "test",
			Name:     "Test",
			Status:   "published",
			Variants: []catalog.Variant{},
		}
		channel := &Channel{
			ID:                "ch1",
			AllowedCurrencies: []string{"USD"},
			PublishedProducts: map[string]bool{"prod1": true},
		}

		_, err := Resolve(product, channel, "USD")
		if err != ErrNoVariantPrice {
			t.Errorf("expected ErrNoVariantPrice, got %v", err)
		}
	})

	t.Run("empty currency", func(t *testing.T) {
		product := &catalog.Product{
			ID:       "prod1",
			Slug:     "test",
			Name:     "Test",
			Status:   "published",
			Variants: []catalog.Variant{},
		}
		channel := &Channel{
			ID:                "ch1",
			AllowedCurrencies: []string{},
			PublishedProducts: map[string]bool{"prod1": true},
		}

		_, err := Resolve(product, channel, "")
		if err != ErrUnknownCurrency {
			t.Errorf("expected ErrUnknownCurrency, got %v", err)
		}
	})
}
