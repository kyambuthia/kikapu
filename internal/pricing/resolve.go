// Package pricing provides price resolution for products across regions and sales channels.
//
// Price Resolution Order:
//
// 1. Base price - The variant's base price
//
// 2. Price list for the region - Region-specific price overrides (planned)
//
// 3. Promotion - Discounts and promotions (planned)
//
// 4. Tax - Applied based on region's TaxRateBPS and TaxInclusive setting
package pricing

import (
	"errors"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/platform/money"
	"github.com/kyambuthia/kikapu/internal/region"
)

var (
	ErrNotPublished        = errors.New("pricing: product not published in channel")
	ErrUnsupportedCurrency = errors.New("pricing: no price for currency")
)

func ResolveForRegion(p catalog.Product, r region.Region, ch channel.SalesChannel) (money.Amount, error) {
	isPublished := false
	for _, regionID := range ch.Regions {
		if regionID == r.ID {
			isPublished = true
			break
		}
	}
	if !isPublished {
		return money.Amount{}, ErrNotPublished
	}

	for _, variant := range p.Variants {
		if variant.Price.Currency == r.Currency {
			return variant.Price, nil
		}
	}

	return money.Amount{}, ErrUnsupportedCurrency
}
