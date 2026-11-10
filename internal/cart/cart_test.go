package cart

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestCart_AddItem(t *testing.T) {
	tests := []struct {
		name       string
		currency   string
		variantID  string
		quantity   int
		unitPrice  money.Amount
		wantErr    error
		wantQty    int
		wantLines  int
	}{
		{
			name:      "add new item",
			currency:  "USD",
			variantID: "var001",
			quantity:  2,
			unitPrice: money.NewAmount(1000, "USD"),
			wantErr:   nil,
			wantQty:   2,
			wantLines: 1,
		},
		{
			name:      "add existing item",
			currency:  "USD",
			variantID: "var001",
			quantity:  3,
			unitPrice: money.NewAmount(1000, "USD"),
			wantErr:   nil,
			wantQty:   5,
			wantLines: 1,
		},
		{
			name:      "quantity too low",
			currency:  "USD",
			variantID: "var002",
			quantity:  0,
			unitPrice: money.NewAmount(1000, "USD"),
			wantErr:   ErrInvalidQuantity,
		},
		{
			name:      "quantity too high",
			currency:  "USD",
			variantID: "var002",
			quantity:  100,
			unitPrice: money.NewAmount(1000, "USD"),
			wantErr:   ErrInvalidQuantity,
		},
		{
			name:      "currency mismatch",
			currency:  "USD",
			variantID: "var002",
			quantity:  1,
			unitPrice: money.NewAmount(1000, "EUR"),
			wantErr:   ErrCurrencyMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cart := NewCart("test-cart", tt.currency)
			if tt.name == "add existing item" {
				cart.AddItem("var001", 2, money.NewAmount(1000, "USD"))
			}

			_, err := cart.AddItem(tt.variantID, tt.quantity, tt.unitPrice)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("AddItem() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("AddItem() unexpected error: %v", err)
				return
			}

			if len(cart.Lines) != tt.wantLines {
				t.Errorf("AddItem() lines = %d, want %d", len(cart.Lines), tt.wantLines)
			}

			for _, line := range cart.Lines {
				if line.VariantID == tt.variantID && line.Quantity != tt.wantQty {
					t.Errorf("AddItem() quantity = %d, want %d", line.Quantity, tt.wantQty)
				}
			}
		})
	}
}

func TestCart_UpdateQuantity(t *testing.T) {
	tests := []struct {
		name      string
		variantID string
		quantity  int
		wantErr   error
		wantQty   int
	}{
		{
			name:      "update existing item",
			variantID: "var001",
			quantity:  5,
			wantErr:   nil,
			wantQty:   5,
		},
		{
			name:      "quantity too low",
			variantID: "var001",
			quantity:  0,
			wantErr:   ErrInvalidQuantity,
		},
		{
			name:      "item not found",
			variantID: "var999",
			quantity:  5,
			wantErr:   ErrItemNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cart := NewCart("test-cart", "USD")
			cart.AddItem("var001", 2, money.NewAmount(1000, "USD"))

			_, err := cart.UpdateQuantity(tt.variantID, tt.quantity)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("UpdateQuantity() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("UpdateQuantity() unexpected error: %v", err)
				return
			}

			for _, line := range cart.Lines {
				if line.VariantID == tt.variantID && line.Quantity != tt.wantQty {
					t.Errorf("UpdateQuantity() quantity = %d, want %d", line.Quantity, tt.wantQty)
				}
			}
		})
	}
}

func TestCart_RemoveItem(t *testing.T) {
	cart := NewCart("test-cart", "USD")
	cart.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cart.AddItem("var002", 1, money.NewAmount(2000, "USD"))

	_, err := cart.RemoveItem("var001")
	if err != nil {
		t.Errorf("RemoveItem() unexpected error: %v", err)
	}

	if len(cart.Lines) != 1 {
		t.Errorf("RemoveItem() lines = %d, want 1", len(cart.Lines))
	}

	_, err = cart.RemoveItem("var999")
	if err != ErrItemNotFound {
		t.Errorf("RemoveItem() error = %v, want %v", err, ErrItemNotFound)
	}
}

func TestCart_Clear(t *testing.T) {
	cart := NewCart("test-cart", "USD")
	cart.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cart.AddItem("var002", 1, money.NewAmount(2000, "USD"))

	version := cart.Clear()

	if len(cart.Lines) != 0 {
		t.Errorf("Clear() lines = %d, want 0", len(cart.Lines))
	}

	if version != cart.Version {
		t.Errorf("Clear() version = %d, want %d", version, cart.Version)
	}
}

func TestCart_Total(t *testing.T) {
	tests := []struct {
		name     string
		lines    []Line
		want     int64
		wantErr  bool
	}{
		{
			name:  "empty cart",
			lines: []Line{},
			want:  0,
		},
		{
			name: "single item",
			lines: []Line{
				{VariantID: "var001", Quantity: 2, UnitPrice: money.NewAmount(1000, "USD")},
			},
			want: 2000,
		},
		{
			name: "multiple items",
			lines: []Line{
				{VariantID: "var001", Quantity: 2, UnitPrice: money.NewAmount(1000, "USD")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(500, "USD")},
			},
			want: 2500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cart := NewCart("test-cart", "USD")
			cart.Lines = tt.lines

			total, err := cart.Total()

			if tt.wantErr {
				if err == nil {
					t.Error("Total() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Total() unexpected error: %v", err)
				return
			}

			if total.Value != tt.want {
				t.Errorf("Total() = %d, want %d", total.Value, tt.want)
			}
		})
	}
}
