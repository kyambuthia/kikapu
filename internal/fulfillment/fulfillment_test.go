package fulfillment

import (
	"testing"
)

func TestFulfillment_Transition(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr error
	}{
		{"pending to picked", StatusPending, StatusPicked, nil},
		{"pending to cancelled", StatusPending, StatusCancelled, nil},
		{"pending to packed", StatusPending, StatusPacked, ErrInvalidTransition},
		{"picked to packed", StatusPicked, StatusPacked, nil},
		{"picked to cancelled", StatusPicked, StatusCancelled, nil},
		{"picked to shipped", StatusPicked, StatusShipped, ErrInvalidTransition},
		{"packed to shipped", StatusPacked, StatusShipped, nil},
		{"packed to cancelled", StatusPacked, StatusCancelled, nil},
		{"packed to delivered", StatusPacked, StatusDelivered, ErrInvalidTransition},
		{"shipped to delivered", StatusShipped, StatusDelivered, nil},
		{"shipped to cancelled", StatusShipped, StatusCancelled, ErrInvalidTransition},
		{"delivered to shipped", StatusDelivered, StatusShipped, ErrInvalidTransition},
		{"cancelled to pending", StatusCancelled, StatusPending, ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fulfillment{Status: tt.from}
			err := f.Transition(tt.to)
			if err != tt.wantErr {
				t.Errorf("Transition() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && f.Status != tt.to {
				t.Errorf("Transition() status = %v, want %v", f.Status, tt.to)
			}
		})
	}
}

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
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
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("Status.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateTrackingURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{"valid https", "https://tracking.example.com/123", nil},
		{"empty url", "", nil},
		{"http not allowed", "http://tracking.example.com/123", ErrInvalidTrackingURL},
		{"invalid url", "not-a-url", ErrInvalidTrackingURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrackingURL(tt.url)
			if err != tt.wantErr {
				t.Errorf("ValidateTrackingURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewFulfillment(t *testing.T) {
	tests := []struct {
		name    string
		items   []Item
		wantErr error
	}{
		{"valid fulfillment", []Item{{VariantID: "var1", Quantity: 1}}, nil},
		{"empty items", []Item{}, ErrItemsEmpty},
		{"nil items", nil, ErrItemsEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFulfillment("ful_123", "ord_123", tt.items)
			if err != tt.wantErr {
				t.Errorf("NewFulfillment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsValidTransition_Table(t *testing.T) {
	transitions := map[Status][]Status{
		StatusPending:   {StatusPicked, StatusCancelled},
		StatusPicked:    {StatusPacked, StatusCancelled},
		StatusPacked:    {StatusShipped, StatusCancelled},
		StatusShipped:   {StatusDelivered},
		StatusDelivered: {},
		StatusCancelled: {},
	}

	for from, allowed := range transitions {
		t.Run(string(from), func(t *testing.T) {
			allStatuses := []Status{
				StatusPending, StatusPicked, StatusPacked,
				StatusShipped, StatusDelivered, StatusCancelled,
			}

			for _, to := range allStatuses {
				isAllowed := false
				for _, a := range allowed {
					if a == to {
						isAllowed = true
						break
					}
				}

				got := isValidTransition(from, to)
				if got != isAllowed {
					t.Errorf("isValidTransition(%s, %s) = %v, want %v", from, to, got, isAllowed)
				}
			}
		})
	}
}
