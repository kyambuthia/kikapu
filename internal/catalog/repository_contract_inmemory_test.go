package catalog

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestInMemoryRepository_Contract(t *testing.T) {
	contract := RepositoryContract{
		NewRepo: func(t *testing.T) Repository {
			products := []Product{
				{
					ID:     "prod-001",
					Slug:   "wireless-mouse",
					Name:   "Wireless Mouse",
					Status: "published",
					Variants: []Variant{
						{ID: "var-001", SKU: "WM-BLK-001", Price: money.NewAmount(2999, "USD"), Currency: "USD"},
					},
				},
				{
					ID:     "prod-002",
					Slug:   "wireless-keyboard",
					Name:   "Wireless Keyboard",
					Status: "published",
					Variants: []Variant{
						{ID: "var-002", SKU: "WK-001", Price: money.NewAmount(5999, "USD"), Currency: "USD"},
					},
				},
			}
			return NewInMemoryRepository(products)
		},
	}

	contract.Run(t)
}
