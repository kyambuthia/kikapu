package channel

import (
	"testing"
)

func TestSalesChannel_Validate(t *testing.T) {
	tests := []struct {
		name    string
		channel SalesChannel
		wantErr error
	}{
		{
			name: "valid channel",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001", "reg-002"},
			},
			wantErr: nil,
		},
		{
			name: "invalid - empty ID",
			channel: SalesChannel{
				ID:      "",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidID,
		},
		{
			name: "invalid - empty name",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "",
				Slug:    "default",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidName,
		},
		{
			name: "invalid - empty slug",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - slug with spaces",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default channel",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - slug with uppercase",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "Default",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - slug starting with hyphen",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "-default",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - slug ending with hyphen",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default-",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - slug too long",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "this-is-a-very-long-slug-that-exceeds-the-maximum-length-limit-by-quite-a-bit-okay-more",
				Regions: []string{"reg-001"},
			},
			wantErr: ErrInvalidSlug,
		},
		{
			name: "invalid - empty region ID in list",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{"reg-001", ""},
			},
			wantErr: ErrInvalidRegion,
		},
		{
			name: "valid - empty regions slice",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "Default",
				Slug:    "default",
				Regions: []string{},
			},
			wantErr: nil,
		},
		{
			name: "valid - complex slug",
			channel: SalesChannel{
				ID:      "ch-001",
				Name:    "East Africa",
				Slug:    "east-africa-2024",
				Regions: []string{"reg-001", "reg-002", "reg-003"},
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.channel.Validate()
			if err != tt.wantErr {
				t.Errorf("SalesChannel.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
