package tax

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/money"
	"github.com/kyambuthia/kikapu/internal/region"
)

func TestRate(t *testing.T) {
	tests := []struct {
		name    string
		region  region.Region
		wantBps int
	}{
		{
			name:    "16_percent_vat",
			region:  region.Region{TaxRateBPS: 1600},
			wantBps: 1600,
		},
		{
			name:    "zero_tax",
			region:  region.Region{TaxRateBPS: 0},
			wantBps: 0,
		},
		{
			name:    "20_percent_vat",
			region:  region.Region{TaxRateBPS: 2000},
			wantBps: 2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Rate(tt.region)
			if got != tt.wantBps {
				t.Errorf("Rate() = %d, want %d", got, tt.wantBps)
			}
		})
	}
}

func TestTaxFor(t *testing.T) {
	tests := []struct {
		name        string
		lines       []order.Line
		bps         int
		inclusive   bool
		wantPerLine []int64
		wantTotal   int64
		wantErr     error
	}{
		{
			name: "100_kes_at_16_percent",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{1600},
			wantTotal:   1600,
		},
		{
			name: "999_kes_at_16_percent_round_half_even",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(99900, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{15984},
			wantTotal:   15984,
		},
		{
			name: "3_lines_with_residual_adjustment",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(3333, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(3333, "KES")},
				{VariantID: "var003", Quantity: 1, UnitPrice: money.NewAmount(3334, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{534, 533, 533},
			wantTotal:   1600,
		},
		{
			name: "inclusive_tax_100_kes",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			bps:         1600,
			inclusive:   true,
			wantPerLine: []int64{1379},
			wantTotal:   1379,
		},
		{
			name: "inclusive_3_lines_with_residual",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(3333, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(3333, "KES")},
				{VariantID: "var003", Quantity: 1, UnitPrice: money.NewAmount(3334, "KES")},
			},
			bps:         1600,
			inclusive:   true,
			wantPerLine: []int64{459, 460, 460},
			wantTotal:   1379,
		},
		{
			name:      "empty_lines_error",
			lines:     []order.Line{},
			bps:       1600,
			inclusive: false,
			wantErr:   ErrEmptyLines,
		},
		{
			name: "currency_mismatch_error",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(10000, "USD")},
			},
			bps:       1600,
			inclusive: false,
			wantErr:   ErrCurrencyMismatch,
		},
		{
			name: "multiple_quantities",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 3, UnitPrice: money.NewAmount(10000, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{4800},
			wantTotal:   4800,
		},
		{
			name: "zero_tax_rate",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			bps:         0,
			inclusive:   false,
			wantPerLine: []int64{0},
			wantTotal:   0,
		},
		{
			name: "round_half_even_case",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(6250, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{1000},
			wantTotal:   1000,
		},
		{
			name: "residual_adjusts_largest_line",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(3333, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(3334, "KES")},
			},
			bps:         1600,
			inclusive:   false,
			wantPerLine: []int64{534, 533},
			wantTotal:   1067,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perLine, total, err := TaxFor(tt.lines, tt.bps, tt.inclusive)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("TaxFor() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("TaxFor() unexpected error: %v", err)
				return
			}

			if len(perLine) != len(tt.wantPerLine) {
				t.Errorf("TaxFor() perLine length = %d, want %d", len(perLine), len(tt.wantPerLine))
				return
			}

			for i, got := range perLine {
				if got.Value != tt.wantPerLine[i] {
					t.Errorf("TaxFor() perLine[%d] = %d, want %d", i, got.Value, tt.wantPerLine[i])
				}
			}

			if total.Value != tt.wantTotal {
				t.Errorf("TaxFor() total = %d, want %d", total.Value, tt.wantTotal)
			}

			if len(tt.lines) > 0 && len(perLine) > 0 {
				sumRounded := int64(0)
				for _, amt := range perLine {
					sumRounded += amt.Value
				}
				if sumRounded != total.Value {
					t.Errorf("TaxFor() sum of perLine = %d, doesn't equal total %d", sumRounded, total.Value)
				}
			}
		})
	}
}

func TestRoundHalfEvenInt(t *testing.T) {
	tests := []struct {
		name       string
		numerator  int64
		denominator int64
		want       int64
	}{
		{
			name:       "round_down",
			numerator:  53328,
			denominator:   100,
			want:       533,
		},
		{
			name:       "round_up",
			numerator:  53344,
			denominator:   100,
			want:       533,
		},
		{
			name:       "exact_half_round_to_even",
			numerator:  100050,
			denominator:   100,
			want:       1000,
		},
		{
			name:       "exact_half_round_to_even_odd",
			numerator:  100150,
			denominator:   100,
			want:       1002,
		},
		{
			name:       "negative_value",
			numerator:  -53328,
			denominator:   100,
			want:       -533,
		},
		{
			name:       "zero",
			numerator:  0,
			denominator:   100,
			want:       0,
		},
		{
			name:       "inclusive_example",
			numerator:  5332800,
			denominator:   11600,
			want:       460,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundHalfEvenInt(tt.numerator, tt.denominator)
			if got != tt.want {
				t.Errorf("roundHalfEvenInt() = %d, want %d", got, tt.want)
			}
		})
	}
}
