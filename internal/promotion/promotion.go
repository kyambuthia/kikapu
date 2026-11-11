package promotion

import (
	"sort"
	"time"

	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type Kind string

const (
	KindPercentage   Kind = "percentage"
	KindFixed        Kind = "fixed"
	KindFreeShipping Kind = "free_shipping"
)

type Promotion struct {
	ID               string
	Code             string
	Kind             Kind
	Value            int
	MinSubtotal      money.Amount
	StartsAt         time.Time
	EndsAt           time.Time
	UsageLimit       int
	PerCustomerLimit int
	AppliesTo        []string
}

type Applied struct {
	PromotionID string
	Code        string
	Amount      money.Amount
	Reason      string
}

type Rejection struct {
	PromotionID string
	Code        string
	Reason      string
}

func Apply(lines []order.Line, promos []Promotion, now time.Time) ([]Applied, []Rejection) {
	var applied []Applied
	var rejections []Rejection

	codePromoUsed := false
	eligibleByKind := make(map[Kind][]Promotion)

	for _, promo := range promos {
		if !isActive(promo, now) {
			rejections = append(rejections, Rejection{
				PromotionID: promo.ID,
				Code:        promo.Code,
				Reason:      "expired_or_not_started",
			})
			continue
		}

		if promo.UsageLimit == 0 {
			rejections = append(rejections, Rejection{
				PromotionID: promo.ID,
				Code:        promo.Code,
				Reason:      "usage_limit_exhausted",
			})
			continue
		}

		if promo.Code != "" && codePromoUsed {
			rejections = append(rejections, Rejection{
				PromotionID: promo.ID,
				Code:        promo.Code,
				Reason:      "one_code_per_order",
			})
			continue
		}

		subtotal := calculateSubtotal(lines)
		if promo.MinSubtotal.Value > 0 && subtotal.Value < promo.MinSubtotal.Value {
			rejections = append(rejections, Rejection{
				PromotionID: promo.ID,
				Code:        promo.Code,
				Reason:      "minimum_subtotal_not_met",
			})
			continue
		}

		eligibleByKind[promo.Kind] = append(eligibleByKind[promo.Kind], promo)
	}

	percentagePromos := eligibleByKind[KindPercentage]
	fixedPromos := eligibleByKind[KindFixed]

	sort.Slice(percentagePromos, func(i, j int) bool {
		return percentagePromos[i].ID < percentagePromos[j].ID
	})

	sort.Slice(fixedPromos, func(i, j int) bool {
		return fixedPromos[i].ID < fixedPromos[j].ID
	})

	allPromos := append(percentagePromos, fixedPromos...)

	for _, promo := range allPromos {
		if promo.Code != "" && codePromoUsed {
			rejections = append(rejections, Rejection{
				PromotionID: promo.ID,
				Code:        promo.Code,
				Reason:      "one_code_per_order",
			})
			continue
		}

		amount := calculateDiscount(lines, promo)
		if amount.Value == 0 {
			continue
		}

		applied = append(applied, Applied{
			PromotionID: promo.ID,
			Code:        promo.Code,
			Amount:      amount,
			Reason:      string(promo.Kind),
		})

		if promo.Code != "" {
			codePromoUsed = true
		}
	}

	return applied, rejections
}

func isActive(promo Promotion, now time.Time) bool {
	return (now.Equal(promo.StartsAt) || now.After(promo.StartsAt)) &&
		(now.Before(promo.EndsAt) && !now.Equal(promo.EndsAt))
}

func calculateSubtotal(lines []order.Line) money.Amount {
	if len(lines) == 0 {
		return money.Amount{}
	}

	currency := lines[0].UnitPrice.Currency
	subtotal := money.NewAmount(0, currency)

	for _, line := range lines {
		lineTotal, err := line.UnitPrice.Mul(int64(line.Quantity))
		if err != nil {
			continue
		}
		subtotal, _ = subtotal.Add(lineTotal)
	}

	return subtotal
}

func calculateDiscount(lines []order.Line, promo Promotion) money.Amount {
	if len(lines) == 0 {
		return money.Amount{}
	}

	currency := lines[0].UnitPrice.Currency

	applicableLines := lines
	if len(promo.AppliesTo) > 0 {
		applicableLines = filterLinesByVariant(lines, promo.AppliesTo)
	}

	if len(applicableLines) == 0 {
		return money.NewAmount(0, currency)
	}

	switch promo.Kind {
	case KindPercentage:
		return calculatePercentageDiscount(applicableLines, promo.Value, currency)
	case KindFixed:
		return calculateFixedDiscount(applicableLines, promo.Value, currency)
	case KindFreeShipping:
		return money.NewAmount(0, currency)
	default:
		return money.NewAmount(0, currency)
	}
}

func filterLinesByVariant(lines []order.Line, variantIDs []string) []order.Line {
	var filtered []order.Line
	for _, line := range lines {
		for _, id := range variantIDs {
			if line.VariantID == id {
				filtered = append(filtered, line)
				break
			}
		}
	}
	return filtered
}

func calculatePercentageDiscount(lines []order.Line, bps int, currency string) money.Amount {
	total := money.NewAmount(0, currency)

	for _, line := range lines {
		lineTotal, err := line.UnitPrice.Mul(int64(line.Quantity))
		if err != nil {
			continue
		}
		discount := lineTotal.Value * int64(bps) / 10000
		total, _ = total.Add(money.NewAmount(discount, currency))
	}

	return total
}

func calculateFixedDiscount(lines []order.Line, amount int, currency string) money.Amount {
	subtotal := calculateSubtotal(lines)
	if int(subtotal.Value) < amount {
		return subtotal
	}
	return money.NewAmount(int64(amount), currency)
}
