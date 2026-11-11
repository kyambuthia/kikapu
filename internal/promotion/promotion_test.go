package promotion

import (
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestApply(t *testing.T) {
	tests := []struct {
		name              string
		lines             []order.Line
		promos            []Promotion
		now               time.Time
		wantAppliedCount  int
		wantRejectCount   int
		wantAppliedAmount int64
		wantRejectReason  string
	}{
		{
			name: "percentage_10_percent_discount",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  1,
			wantRejectCount:   0,
			wantAppliedAmount: 1000,
		},
		{
			name: "fixed_50_kes_discount",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindFixed,
					Value:      5000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  1,
			wantRejectCount:   0,
			wantAppliedAmount: 5000,
		},
		{
			name: "stacking_percentage_then_fixed",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo002",
					Code:       "",
					Kind:       KindFixed,
					Value:      5000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  2,
			wantRejectCount:   0,
			wantAppliedAmount: 6000,
		},
		{
			name: "two_code_promos_second_rejected",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "CODE10",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
				{
					ID:         "promo002",
					Code:       "CODE20",
					Kind:       KindPercentage,
					Value:      2000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 1,
			wantRejectCount:  1,
			wantRejectReason: "one_code_per_order",
		},
		{
			name: "expired_promotion",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  1,
			wantRejectReason: "expired_or_not_started",
		},
		{
			name: "not_started_promotion",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  1,
			wantRejectReason: "expired_or_not_started",
		},
		{
			name: "starts_at_boundary_active",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  1,
			wantRejectCount:   0,
			wantAppliedAmount: 1000,
		},
		{
			name: "ends_at_boundary_expired",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  1,
			wantRejectReason: "expired_or_not_started",
		},
		{
			name: "usage_limit_exhausted",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 0,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  1,
			wantRejectReason: "usage_limit_exhausted",
		},
		{
			name: "minimum_subtotal_not_met",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(5000, "KES")},
			},
			promos: []Promotion{
				{
					ID:          "promo001",
					Code:        "",
					Kind:        KindPercentage,
					Value:       1000,
					MinSubtotal: money.NewAmount(10000, "KES"),
					StartsAt:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:      time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit:  100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  1,
			wantRejectReason: "minimum_subtotal_not_met",
		},
		{
			name: "applies_to_specific_variants",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(20000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					AppliesTo:  []string{"var001"},
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  1,
			wantRejectCount:   0,
			wantAppliedAmount: 1000,
		},
		{
			name: "fixed_discount_exceeds_subtotal",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(3000, "KES")},
			},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindFixed,
					Value:      5000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:               time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount:  1,
			wantRejectCount:   0,
			wantAppliedAmount: 3000,
		},
		{
			name:  "empty_lines",
			lines: []order.Line{},
			promos: []Promotion{
				{
					ID:         "promo001",
					Code:       "",
					Kind:       KindPercentage,
					Value:      1000,
					StartsAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndsAt:     time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
					UsageLimit: 100,
				},
			},
			now:              time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAppliedCount: 0,
			wantRejectCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applied, rejections := Apply(tt.lines, tt.promos, tt.now)

			if got := len(applied); got != tt.wantAppliedCount {
				t.Errorf("applied count = %d, want %d", got, tt.wantAppliedCount)
			}

			if got := len(rejections); got != tt.wantRejectCount {
				t.Errorf("rejections count = %d, want %d", got, tt.wantRejectCount)
			}

			if tt.wantAppliedAmount > 0 && len(applied) > 0 {
				totalDiscount := int64(0)
				for _, a := range applied {
					totalDiscount += a.Amount.Value
				}
				if totalDiscount != tt.wantAppliedAmount {
					t.Errorf("total discount = %d, want %d", totalDiscount, tt.wantAppliedAmount)
				}
			}

			if tt.wantRejectReason != "" && len(rejections) > 0 {
				found := false
				for _, r := range rejections {
					if r.Reason == tt.wantRejectReason {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected rejection reason %q not found", tt.wantRejectReason)
				}
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	tests := []struct {
		name    string
		promo   Promotion
		now     time.Time
		wantAct bool
	}{
		{
			name: "active_in_range",
			promo: Promotion{
				StartsAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAct: true,
		},
		{
			name: "before_start",
			promo: Promotion{
				StartsAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:     time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
			wantAct: false,
		},
		{
			name: "exactly_at_start",
			promo: Promotion{
				StartsAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAct: true,
		},
		{
			name: "exactly_at_end",
			promo: Promotion{
				StartsAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			now:     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantAct: false,
		},
		{
			name: "after_end",
			promo: Promotion{
				StartsAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			now:     time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			wantAct: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isActive(tt.promo, tt.now)
			if got != tt.wantAct {
				t.Errorf("isActive() = %v, want %v", got, tt.wantAct)
			}
		})
	}
}

func TestCalculateSubtotal(t *testing.T) {
	tests := []struct {
		name      string
		lines     []order.Line
		wantTotal int64
	}{
		{
			name: "single_line",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 2, UnitPrice: money.NewAmount(5000, "KES")},
			},
			wantTotal: 10000,
		},
		{
			name: "multiple_lines",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 2, UnitPrice: money.NewAmount(5000, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(3000, "KES")},
			},
			wantTotal: 13000,
		},
		{
			name:      "empty_lines",
			lines:     []order.Line{},
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSubtotal(tt.lines)
			if got.Value != tt.wantTotal {
				t.Errorf("calculateSubtotal() = %d, want %d", got.Value, tt.wantTotal)
			}
		})
	}
}

func TestCalculateDiscount(t *testing.T) {
	tests := []struct {
		name       string
		lines      []order.Line
		promo      Promotion
		wantAmount int64
	}{
		{
			name: "percentage_10_percent",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promo: Promotion{
				Kind:  KindPercentage,
				Value: 1000,
			},
			wantAmount: 1000,
		},
		{
			name: "fixed_50_kes",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promo: Promotion{
				Kind:  KindFixed,
				Value: 5000,
			},
			wantAmount: 5000,
		},
		{
			name: "applies_to_subset",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
				{VariantID: "var002", Quantity: 1, UnitPrice: money.NewAmount(20000, "KES")},
			},
			promo: Promotion{
				Kind:      KindPercentage,
				Value:     1000,
				AppliesTo: []string{"var001"},
			},
			wantAmount: 1000,
		},
		{
			name: "free_shipping_zero_discount",
			lines: []order.Line{
				{VariantID: "var001", Quantity: 1, UnitPrice: money.NewAmount(10000, "KES")},
			},
			promo: Promotion{
				Kind: KindFreeShipping,
			},
			wantAmount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateDiscount(tt.lines, tt.promo)
			if got.Value != tt.wantAmount {
				t.Errorf("calculateDiscount() = %d, want %d", got.Value, tt.wantAmount)
			}
		})
	}
}
