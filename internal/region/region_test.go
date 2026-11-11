package region

import (
	"testing"
)

func TestRegion_Validate(t *testing.T) {
	tests := []struct {
		name    string
		region  Region
		wantErr error
	}{
		{
			name: "valid region",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: nil,
		},
		{
			name: "invalid - empty ID",
			region: Region{
				ID:           "",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidID,
		},
		{
			name: "invalid - empty name",
			region: Region{
				ID:           "reg-001",
				Name:         "",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidName,
		},
		{
			name: "invalid - bad currency format",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "US",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidCurrency,
		},
		{
			name: "invalid - lowercase currency",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "kes",
				Countries:    []string{"KE"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidCurrency,
		},
		{
			name: "invalid - bad country code",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KEN"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidCountry,
		},
		{
			name: "invalid - lowercase country code",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"ke"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidCountry,
		},
		{
			name: "invalid - negative tax rate",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   -100,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidTaxRate,
		},
		{
			name: "invalid - tax rate above 100%",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   10500,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidTaxRate,
		},
		{
			name: "valid - tax rate at 100%",
			region: Region{
				ID:           "reg-001",
				Name:         "Kenya",
				Currency:     "KES",
				Countries:    []string{"KE"},
				TaxRateBPS:   10000,
				TaxInclusive: true,
			},
			wantErr: nil,
		},
		{
			name: "valid - empty countries slice",
			region: Region{
				ID:           "reg-001",
				Name:         "Global",
				Currency:     "USD",
				Countries:    []string{},
				TaxRateBPS:   0,
				TaxInclusive: false,
			},
			wantErr: nil,
		},
		{
			name: "valid - zero tax rate",
			region: Region{
				ID:           "reg-001",
				Name:         "Tax Free",
				Currency:     "USD",
				Countries:    []string{"AE"},
				TaxRateBPS:   0,
				TaxInclusive: false,
			},
			wantErr: nil,
		},
		{
			name: "valid - multiple countries",
			region: Region{
				ID:           "reg-001",
				Name:         "East Africa",
				Currency:     "KES",
				Countries:    []string{"KE", "UG", "TZ", "RW"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: nil,
		},
		{
			name: "invalid - one bad country in list",
			region: Region{
				ID:           "reg-001",
				Name:         "East Africa",
				Currency:     "KES",
				Countries:    []string{"KE", "UGANDA", "TZ"},
				TaxRateBPS:   1600,
				TaxInclusive: true,
			},
			wantErr: ErrInvalidCountry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.region.Validate()
			if err != tt.wantErr {
				t.Errorf("Region.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
