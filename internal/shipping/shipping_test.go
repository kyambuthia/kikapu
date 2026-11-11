package shipping

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestZoneMatchesCountry(t *testing.T) {
	tests := []struct {
		name     string
		zone     Zone
		country  string
		expected bool
	}{
		{
			name:     "match first country",
			zone:     Zone{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}},
			country:  "KE",
			expected: true,
		},
		{
			name:     "match middle country",
			zone:     Zone{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}},
			country:  "UG",
			expected: true,
		},
		{
			name:     "match last country",
			zone:     Zone{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}},
			country:  "TZ",
			expected: true,
		},
		{
			name:     "no match",
			zone:     Zone{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}},
			country:  "NG",
			expected: false,
		},
		{
			name:     "empty countries list",
			zone:     Zone{ID: "empty", Name: "Empty", Countries: []string{}},
			country:  "KE",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.zone.MatchesCountry(tt.country)
			if got != tt.expected {
				t.Errorf("Zone.MatchesCountry(%s) = %v, want %v", tt.country, got, tt.expected)
			}
		})
	}
}

func TestFindZone(t *testing.T) {
	zones := []Zone{
		{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}},
		{ID: "west-africa", Name: "West Africa", Countries: []string{"NG", "GH"}},
	}

	matcher := NewZoneMatcher(zones)

	tests := []struct {
		name        string
		country     string
		expectedID  string
		expectError bool
	}{
		{
			name:        "find Kenya",
			country:     "KE",
			expectedID:  "east-africa",
			expectError: false,
		},
		{
			name:        "find Uganda",
			country:     "UG",
			expectedID:  "east-africa",
			expectError: false,
		},
		{
			name:        "find Nigeria",
			country:     "NG",
			expectedID:  "west-africa",
			expectError: false,
		},
		{
			name:        "unknown country",
			country:     "XX",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone, err := matcher.FindZone(tt.country)
			if tt.expectError {
				if err != ErrNoShippingZone {
					t.Errorf("expected ErrNoShippingZone, got %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if zone.ID != tt.expectedID {
				t.Errorf("FindZone(%s) = %s, want %s", tt.country, zone.ID, tt.expectedID)
			}
		})
	}
}

func TestRatesFor(t *testing.T) {
	newCart := func(lines []cart.Line) *cart.Cart {
		return &cart.Cart{
			ID:       "test-cart",
			Currency: "USD",
			Lines:    lines,
		}
	}

	newLine := func(variantID string, quantity int, price int64) cart.Line {
		return cart.Line{
			VariantID: variantID,
			Quantity:  quantity,
			UnitPrice: money.NewAmount(price, "USD"),
		}
	}

	zone := Zone{ID: "east-africa", Name: "East Africa", Countries: []string{"KE", "UG", "TZ"}}

	tests := []struct {
		name          string
		zone          Zone
		rates         []Rate
		cart          *cart.Cart
		subtotal      money.Amount
		expectedCount int
		expectedIDs   []string
		expectError   bool
	}{
		{
			name: "flat rate applies",
			zone: zone,
			rates: []Rate{
				{ID: "rate-flat", ZoneID: "east-africa", Name: "Standard", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
			},
			cart:          newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 1,
			expectedIDs:   []string{"rate-flat"},
		},
		{
			name: "free over threshold equal qualifies",
			zone: zone,
			rates: []Rate{
				{ID: "rate-free", ZoneID: "east-africa", Name: "Free Shipping", Kind: KindFreeOver, FreeOver: money.NewAmount(1000, "USD")},
			},
			cart:          newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 1,
			expectedIDs:   []string{"rate-free"},
		},
		{
			name: "free over threshold below does not qualify",
			zone: zone,
			rates: []Rate{
				{ID: "rate-free", ZoneID: "east-africa", Name: "Free Shipping", Kind: KindFreeOver, FreeOver: money.NewAmount(1000, "USD")},
			},
			cart:          newCart([]cart.Line{newLine("v1", 1, 900)}),
			subtotal:      money.NewAmount(900, "USD"),
			expectedCount: 0,
		},
		{
			name: "weight rate applies at threshold",
			zone: zone,
			rates: []Rate{
				{ID: "rate-weight", ZoneID: "east-africa", Name: "Heavy", Kind: KindWeight, Amount: money.NewAmount(1000, "USD"), MinWeightG: 2000},
			},
			cart:          newCart([]cart.Line{newLine("v1", 2, 1000)}),
			subtotal:      money.NewAmount(2000, "USD"),
			expectedCount: 1,
			expectedIDs:   []string{"rate-weight"},
		},
		{
			name: "free over beats flat and weight",
			zone: zone,
			rates: []Rate{
				{ID: "rate-free", ZoneID: "east-africa", Name: "Free", Kind: KindFreeOver, FreeOver: money.NewAmount(1000, "USD")},
				{ID: "rate-flat", ZoneID: "east-africa", Name: "Flat", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
				{ID: "rate-weight", ZoneID: "east-africa", Name: "Weight", Kind: KindWeight, Amount: money.NewAmount(800, "USD"), MinWeightG: 1000},
			},
			cart:          newCart([]cart.Line{newLine("v1", 2, 500)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 3,
			expectedIDs:   []string{"rate-free", "rate-flat", "rate-weight"},
		},
		{
			name: "ties broken by cheapest then ID",
			zone: zone,
			rates: []Rate{
				{ID: "rate-a", ZoneID: "east-africa", Name: "Rate A", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
				{ID: "rate-b", ZoneID: "east-africa", Name: "Rate B", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
				{ID: "rate-c", ZoneID: "east-africa", Name: "Rate C", Kind: KindFlat, Amount: money.NewAmount(300, "USD")},
			},
			cart:          newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 3,
			expectedIDs:   []string{"rate-c", "rate-a", "rate-b"},
		},
		{
			name: "rates from different zone ignored",
			zone: zone,
			rates: []Rate{
				{ID: "rate-flat", ZoneID: "east-africa", Name: "Standard", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
				{ID: "rate-other", ZoneID: "west-africa", Name: "Other", Kind: KindFlat, Amount: money.NewAmount(100, "USD")},
			},
			cart:          newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 1,
			expectedIDs:   []string{"rate-flat"},
		},
		{
			name:          "empty rates returns empty list",
			zone:          zone,
			rates:         []Rate{},
			cart:          newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:      money.NewAmount(1000, "USD"),
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applicable, err := RatesFor(tt.zone, tt.rates, tt.cart, tt.subtotal, "USD")

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(applicable) != tt.expectedCount {
				t.Errorf("got %d rates, want %d", len(applicable), tt.expectedCount)
				return
			}

			if len(tt.expectedIDs) > 0 {
				for i, id := range tt.expectedIDs {
					if applicable[i].RateID != id {
						t.Errorf("rate[%d] = %s, want %s", i, applicable[i].RateID, id)
					}
				}
			}
		})
	}
}

func TestEvaluateRate(t *testing.T) {
	newCart := func(lines []cart.Line) *cart.Cart {
		return &cart.Cart{
			ID:       "test-cart",
			Currency: "USD",
			Lines:    lines,
		}
	}

	newLine := func(variantID string, quantity int, price int64) cart.Line {
		return cart.Line{
			VariantID: variantID,
			Quantity:  quantity,
			UnitPrice: money.NewAmount(price, "USD"),
		}
	}

	tests := []struct {
		name           string
		rate           Rate
		cart           *cart.Cart
		subtotal       money.Amount
		expectApply    bool
		expectedAmt    int64
		expectedReason string
	}{
		{
			name:           "flat rate always applies",
			rate:           Rate{ID: "flat", ZoneID: "z1", Name: "Flat", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
			cart:           newCart([]cart.Line{newLine("v1", 1, 100)}),
			subtotal:       money.NewAmount(100, "USD"),
			expectApply:    true,
			expectedAmt:    500,
			expectedReason: "flat",
		},
		{
			name:           "free over applies at threshold",
			rate:           Rate{ID: "free", ZoneID: "z1", Name: "Free", Kind: KindFreeOver, FreeOver: money.NewAmount(1000, "USD")},
			cart:           newCart([]cart.Line{newLine("v1", 1, 1000)}),
			subtotal:       money.NewAmount(1000, "USD"),
			expectApply:    true,
			expectedAmt:    0,
			expectedReason: "free_over",
		},
		{
			name:        "free over does not apply below threshold",
			rate:        Rate{ID: "free", ZoneID: "z1", Name: "Free", Kind: KindFreeOver, FreeOver: money.NewAmount(1000, "USD")},
			cart:        newCart([]cart.Line{newLine("v1", 1, 900)}),
			subtotal:    money.NewAmount(900, "USD"),
			expectApply: false,
		},
		{
			name:           "weight rate applies at min weight",
			rate:           Rate{ID: "weight", ZoneID: "z1", Name: "Weight", Kind: KindWeight, Amount: money.NewAmount(1000, "USD"), MinWeightG: 2000},
			cart:           newCart([]cart.Line{newLine("v1", 2, 500)}),
			subtotal:       money.NewAmount(1000, "USD"),
			expectApply:    true,
			expectedAmt:    1000,
			expectedReason: "weight",
		},
		{
			name:        "weight rate does not apply below min weight",
			rate:        Rate{ID: "weight", ZoneID: "z1", Name: "Weight", Kind: KindWeight, Amount: money.NewAmount(1000, "USD"), MinWeightG: 5000},
			cart:        newCart([]cart.Line{newLine("v1", 1, 500)}),
			subtotal:    money.NewAmount(500, "USD"),
			expectApply: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, ok, err := evaluateRate(tt.rate, tt.cart, tt.subtotal, "USD")

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if ok != tt.expectApply {
				t.Errorf("expected apply = %v, got %v", tt.expectApply, ok)
				return
			}

			if !tt.expectApply {
				return
			}

			if app.Amount.Value != tt.expectedAmt {
				t.Errorf("amount = %d, want %d", app.Amount.Value, tt.expectedAmt)
			}

			if app.Reason != tt.expectedReason {
				t.Errorf("reason = %s, want %s", app.Reason, tt.expectedReason)
			}
		})
	}
}
