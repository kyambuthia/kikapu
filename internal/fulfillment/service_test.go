package fulfillment

import (
	"errors"
	"testing"
)

type mockOrderRepo struct {
	order  Order
	status string
	err    error
}

func (m *mockOrderRepo) Get(id string) (Order, error) {
	return m.order, m.err
}

func (m *mockOrderRepo) UpdateFulfillmentStatus(id string, status string) error {
	m.status = status
	return nil
}

type mockFulfillmentRepo struct {
	fulfillments []*Fulfillment
	created      *Fulfillment
	updated      *Fulfillment
	tracking     *Tracking
	err          error
}

func (m *mockFulfillmentRepo) Create(f *Fulfillment) error {
	m.created = f
	return m.err
}

func (m *mockFulfillmentRepo) Get(id string) (*Fulfillment, error) {
	for _, f := range m.fulfillments {
		if f.ID == id {
			return f, nil
		}
	}
	return nil, ErrFulfillmentNotFound
}

func (m *mockFulfillmentRepo) ListByOrder(orderID string) ([]*Fulfillment, error) {
	var result []*Fulfillment
	for _, f := range m.fulfillments {
		if f.OrderID == orderID {
			result = append(result, f)
		}
	}
	return result, m.err
}

func (m *mockFulfillmentRepo) Update(f *Fulfillment) error {
	m.updated = f
	for i, existing := range m.fulfillments {
		if existing.ID == f.ID {
			m.fulfillments[i] = f
		}
	}
	return m.err
}

func (m *mockFulfillmentRepo) AddTracking(id string, t Tracking) error {
	m.tracking = &t
	for _, f := range m.fulfillments {
		if f.ID == id {
			f.Tracking = &t
		}
	}
	return m.err
}

func TestService_Create_OverFulfillment(t *testing.T) {
	tests := []struct {
		name         string
		order        Order
		existing     []*Fulfillment
		items        []Item
		wantErr      error
		wantOrderErr bool
	}{
		{
			name: "exact fulfillment",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 10}},
			wantErr: nil,
		},
		{
			name: "partial fulfillment",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 5}},
			wantErr: nil,
		},
		{
			name: "over fulfillment single item",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 15}},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "partial then remainder",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			existing: []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusShipped, Items: []Item{{VariantID: "var1", Quantity: 6}}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 4}},
			wantErr: nil,
		},
		{
			name: "over after partial",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			existing: []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusShipped, Items: []Item{{VariantID: "var1", Quantity: 6}}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 5}},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "cancelled fulfilment doesn't count",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			existing: []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusCancelled, Items: []Item{{VariantID: "var1", Quantity: 6}}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 10}},
			wantErr: nil,
		},
		{
			name: "order not confirmed",
			order: Order{
				ID:     "ord_123",
				Status: "pending",
				Lines:  []OrderLine{{VariantID: "var1", Quantity: 10}},
			},
			items:   []Item{{VariantID: "var1", Quantity: 5}},
			wantErr: ErrOrderNotConfirmed,
		},
		{
			name: "multiple variants",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var1", Quantity: 10},
					{VariantID: "var2", Quantity: 5},
				},
			},
			existing: []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusShipped, Items: []Item{{VariantID: "var1", Quantity: 6}}},
			},
			items: []Item{
				{VariantID: "var1", Quantity: 4},
				{VariantID: "var2", Quantity: 5},
			},
			wantErr: nil,
		},
		{
			name: "over fulfillment multiple variants",
			order: Order{
				ID:     "ord_123",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var1", Quantity: 10},
					{VariantID: "var2", Quantity: 5},
				},
			},
			existing: []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusShipped, Items: []Item{{VariantID: "var1", Quantity: 6}}},
			},
			items: []Item{
				{VariantID: "var1", Quantity: 5},
				{VariantID: "var2", Quantity: 5},
			},
			wantErr: ErrOverFulfillment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderRepo := &mockOrderRepo{order: tt.order}
			fulfillmentRepo := &mockFulfillmentRepo{fulfillments: tt.existing}

			service := NewService(fulfillmentRepo, orderRepo)
			_, err := service.Create(nil, tt.order.ID, tt.items)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_AddTracking_HTTPSOnly(t *testing.T) {
	tests := []struct {
		name     string
		tracking Tracking
		wantErr  error
	}{
		{"valid https", Tracking{Carrier: "DHL", Number: "123456", URL: "https://dhl.com/track"}, nil},
		{"empty url", Tracking{Carrier: "DHL", Number: "123456", URL: ""}, nil},
		{"http rejected", Tracking{Carrier: "DHL", Number: "123456", URL: "http://dhl.com/track"}, ErrInvalidTrackingURL},
		{"missing carrier", Tracking{Carrier: "", Number: "123456", URL: ""}, ErrMissingCarrier},
		{"missing number", Tracking{Carrier: "DHL", Number: "", URL: ""}, ErrMissingTrackingNumber},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fulfillments := []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: StatusShipped},
			}
			fulfillmentRepo := &mockFulfillmentRepo{fulfillments: fulfillments}
			orderRepo := &mockOrderRepo{}

			service := NewService(fulfillmentRepo, orderRepo)
			err := service.AddTracking(nil, "ful_1", tt.tracking)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AddTracking() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_UpdateStatus_Transitions(t *testing.T) {
	tests := []struct {
		name       string
		from       Status
		to         Status
		wantErr    error
		wantStatus Status
	}{
		{"pending to picked", StatusPending, StatusPicked, nil, StatusPicked},
		{"pending to packed", StatusPending, StatusPacked, ErrInvalidTransition, StatusPending},
		{"picked to packed", StatusPicked, StatusPacked, nil, StatusPacked},
		{"shipped to delivered", StatusShipped, StatusDelivered, nil, StatusDelivered},
		{"delivered to shipped", StatusDelivered, StatusShipped, ErrInvalidTransition, StatusDelivered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fulfillments := []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: tt.from},
			}
			fulfillmentRepo := &mockFulfillmentRepo{fulfillments: fulfillments}
			orderRepo := &mockOrderRepo{}

			service := NewService(fulfillmentRepo, orderRepo)
			err := service.UpdateStatus(nil, "ful_1", tt.to)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("UpdateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && fulfillments[0].Status != tt.wantStatus {
				t.Errorf("UpdateStatus() status = %v, want %v", fulfillments[0].Status, tt.wantStatus)
			}
		})
	}
}

func TestService_AddTracking_OnlyForShipped(t *testing.T) {
	tests := []struct {
		name    string
		status  Status
		wantErr error
	}{
		{"pending", StatusPending, ErrInvalidTransition},
		{"picked", StatusPicked, ErrInvalidTransition},
		{"packed", StatusPacked, ErrInvalidTransition},
		{"shipped", StatusShipped, nil},
		{"delivered", StatusDelivered, ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fulfillments := []*Fulfillment{
				{ID: "ful_1", OrderID: "ord_123", Status: tt.status},
			}
			fulfillmentRepo := &mockFulfillmentRepo{fulfillments: fulfillments}
			orderRepo := &mockOrderRepo{}

			service := NewService(fulfillmentRepo, orderRepo)
			tracking := Tracking{Carrier: "DHL", Number: "123", URL: "https://dhl.com"}
			err := service.AddTracking(nil, "ful_1", tracking)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AddTracking() status=%s error = %v, wantErr %v", tt.status, err, tt.wantErr)
			}
		})
	}
}
