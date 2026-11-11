package fulfillment

import (
	"testing"
)

func TestTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr error
	}{
		{"pending to picked", StatusPending, StatusPicked, nil},
		{"pending to cancelled", StatusPending, StatusCancelled, nil},
		{"picked to packed", StatusPicked, StatusPacked, nil},
		{"picked to cancelled", StatusPicked, StatusCancelled, nil},
		{"packed to shipped", StatusPacked, StatusShipped, nil},
		{"packed to cancelled", StatusPacked, StatusCancelled, nil},
		{"shipped to delivered", StatusShipped, StatusDelivered, nil},
		{"pending to shipped", StatusPending, StatusShipped, ErrInvalidTransition},
		{"pending to delivered", StatusPending, StatusDelivered, ErrInvalidTransition},
		{"picked to delivered", StatusPicked, StatusDelivered, ErrInvalidTransition},
		{"cancelled to pending", StatusCancelled, StatusPending, ErrInvalidTransition},
		{"delivered to pending", StatusDelivered, StatusPending, ErrInvalidTransition},
		{"shipped to cancelled", StatusShipped, StatusCancelled, ErrInvalidTransition},
		{"delivered to cancelled", StatusDelivered, StatusCancelled, ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fulfillment{Status: tt.from}
			err := f.Transition(tt.to)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Transition() error = %v, want %v", err, tt.wantErr)
				}
				if f.Status != tt.from {
					t.Errorf("Transition() status changed from %v to %v", tt.from, f.Status)
				}
			} else {
				if err != nil {
					t.Errorf("Transition() unexpected error = %v", err)
				}
				if f.Status != tt.to {
					t.Errorf("Transition() status = %v, want %v", f.Status, tt.to)
				}
			}
		})
	}
}

func TestAddTracking(t *testing.T) {
	tests := []struct {
		name     string
		tracking Tracking
		wantErr  error
	}{
		{
			name: "valid https url",
			tracking: Tracking{
				Carrier: "DHL",
				Number:  "123456",
				URL:     "https://track.dhl.com",
			},
			wantErr: nil,
		},
		{
			name: "invalid http url",
			tracking: Tracking{
				Carrier: "DHL",
				Number:  "123456",
				URL:     "http://track.dhl.com",
			},
			wantErr: ErrInvalidTrackingURL,
		},
		{
			name: "empty url",
			tracking: Tracking{
				Carrier: "DHL",
				Number:  "123456",
				URL:     "",
			},
			wantErr: ErrInvalidTrackingURL,
		},
		{
			name: "empty tracking number",
			tracking: Tracking{
				Carrier: "DHL",
				Number:  "",
				URL:     "https://track.dhl.com",
			},
			wantErr: ErrInvalidTrackingNumber,
		},
		{
			name: "empty carrier",
			tracking: Tracking{
				Carrier: "",
				Number:  "123456",
				URL:     "https://track.dhl.com",
			},
			wantErr: ErrInvalidCarrier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fulfillment{}
			err := f.AddTracking(tt.tracking)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("AddTracking() error = %v, want %v", err, tt.wantErr)
				}
				if f.Tracking != nil {
					t.Errorf("AddTracking() tracking should not be set on error")
				}
			} else {
				if err != nil {
					t.Errorf("AddTracking() unexpected error = %v", err)
				}
				if f.Tracking == nil {
					t.Errorf("AddTracking() tracking is nil")
				}
				if f.Tracking.Carrier != tt.tracking.Carrier {
					t.Errorf("AddTracking() carrier = %v, want %v", f.Tracking.Carrier, tt.tracking.Carrier)
				}
				if f.Tracking.Number != tt.tracking.Number {
					t.Errorf("AddTracking() number = %v, want %v", f.Tracking.Number, tt.tracking.Number)
				}
				if f.Tracking.URL != tt.tracking.URL {
					t.Errorf("AddTracking() url = %v, want %v", f.Tracking.URL, tt.tracking.URL)
				}
			}
		})
	}
}

func TestStatusValidation(t *testing.T) {
	tests := []struct {
		status Status
		valid  bool
	}{
		{StatusPending, true},
		{StatusPicked, true},
		{StatusPacked, true},
		{StatusShipped, true},
		{StatusDelivered, true},
		{StatusCancelled, true},
		{Status("invalid"), false},
		{Status(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.valid {
				t.Errorf("Status(%s).IsValid() = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestIsHTTPS(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com", false},
		{"ftp://example.com", false},
		{"", false},
		{"not a url", false},
		{"https://", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isHTTPS(tt.url); got != tt.want {
				t.Errorf("isHTTPS(%s) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
