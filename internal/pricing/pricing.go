package pricing

import (
	"errors"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrUnknownCurrency     = errors.New("unknown currency")
	ErrProductNotPublished = errors.New("product not published in channel")
	ErrNoVariantPrice      = errors.New("no variant price available")
)

type Channel struct {
	ID                string
	Name              string
	AllowedCurrencies []string
	PublishedProducts map[string]bool
}

func (c *Channel) IsCurrencyAllowed(currency string) bool {
	for _, curr := range c.AllowedCurrencies {
		if curr == currency {
			return true
		}
	}
	return false
}

func (c *Channel) IsProductPublished(productID string) bool {
	return c.PublishedProducts[productID]
}

func Resolve(product *catalog.Product, channel *Channel, currency string) (money.Amount, error) {
	if !channel.IsProductPublished(product.ID) {
		return money.Amount{}, ErrProductNotPublished
	}

	if !channel.IsCurrencyAllowed(currency) {
		return money.Amount{}, ErrUnknownCurrency
	}

	for _, variant := range product.Variants {
		if variant.Price.Currency == currency {
			return variant.Price, nil
		}
	}

	return money.Amount{}, ErrNoVariantPrice
}
