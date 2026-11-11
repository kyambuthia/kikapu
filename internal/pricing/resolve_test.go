package pricing

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/platform/money"
	"github.com/kyambuthia/kikapu/internal/region"
)

func TestResolveForRegion(t *testing.T) {
	tests := []struct {
		name    string
		product catalog.Product
		region  region.Region
		channel channel.SalesChannel
		want    money.Amount
		wantErr error
	}{
		{
			name: "happy path - product published in channel with matching currency",
			product: catalog.Product{
				ID:     "prod-001",
				Slug:   "test-product",
				Status: "published",
				Variants: []catalog.Variant{
					{ID: "var-001", SKU: "SKU-001", Price: money.Amount{Value: 10000, Currency: "KES"}, Currency: "KES"},
				},
			},
			region: region.Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			channel: channel.SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			want:    money.Amount{Value: 10000, Currency: "KES"},
			wantErr: nil,
		},
		{
			name: "unpublished channel - region not in channel",
			product: catalog.Product{
				ID:     "prod-001",
				Slug:   "test-product",
				Status: "published",
				Variants: []catalog.Variant{
					{ID: "var-001", SKU: "SKU-001", Price: money.Amount{Value: 10000, Currency: "KES"}, Currency: "KES"},
				},
			},
			region: region.Region{
				ID:           "reg-002",
				Name:         "Uganda",
				Currency:     "UGX",
				Countries:    []string{"UG"},
				TaxRateBPS:   1800,
				TaxInclusive: false,
			},
			channel: channel.SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			want:    money.Amount{},
			wantErr: ErrNotPublished,
		},
		{
			name: "currency missing - product has different currency",
			product: catalog.Product{
				ID:     "prod-001",
				Slug:   "test-product",
				Status: "published",
				Variants: []catalog.Variant{
					{ID: "var-001", SKU: "SKU-001", Price: money.Amount{Value: 10000, Currency: "USD"}, Currency: "USD"},
				},
			},
			region: region.Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			channel: channel.SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			want:    money.Amount{},
			wantErr: ErrUnsupportedCurrency,
		},
		{
			name: "multiple variants - picks matching currency",
			product: catalog.Product{
				ID:     "prod-001",
				Slug:   "test-product",
				Status: "published",
				Variants: []catalog.Variant{
					{ID: "var-001", SKU: "SKU-001", Price: money.Amount{Value: 10000, Currency: "USD"}, Currency: "USD"},
					{ID: "var-002", SKU: "SKU-002", Price: money.Amount{Value: 5000, Currency: "KES"}, Currency: "KES"},
					{ID: "var-003", SKU: "SKU-003", Price: money.Amount{Value: 8000, Currency: "EUR"}, Currency: "EUR"},
				},
			},
			region: region.Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			channel: channel.SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			want:    money.Amount{Value: 5000, Currency: "KES"},
			wantErr: nil,
		},
		{
			name: "multiple regions in channel - product published",
			product: catalog.Product{
				ID:     "prod-001",
				Slug:   "test-product",
				Status: "published",
				Variants: []catalog.Variant{
					{ID: "var-001", SKU: "SKU-001", Price: money.Amount{Value: 10000, Currency: "KES"}, Currency: "KES"},
				},
			},
			region: region.Region{
				ID:           "reg-002",
				Name:         "Uganda",
				Currency:     "KES",
				Countries:    []string{"UG"},
				TaxRateBPS:   1800,
				TaxInclusive: false,
			},
			channel: channel.SalesChannel{
				ID:      "ch-001",
				Name:    "East Africa",
				Slug:    "east-africa",
				Regions: []string{"reg-001", "reg-002", "reg-003"},
			},
			want:    money.Amount{Value: 10000, Currency: "KES"},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveForRegion(tt.product, tt.region, tt.channel)
			if err != tt.wantErr {
				t.Errorf("ResolveForRegion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveForRegion() = %v, want %v", got, tt.want)
			}
		})
	}
}
