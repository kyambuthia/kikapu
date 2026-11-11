package fulfillment

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockRepo struct {
	fulfillments map[string]*Fulfillment
	createErr    error
	getErr       error
	updateErr    error
	listErr      error
}

func (m *mockRepo) Create(ctx context.Context, f *Fulfillment) error {
	if m.createErr != nil {
		return m.createErr
	}
	if m.fulfillments == nil {
		m.fulfillments = make(map[string]*Fulfillment)
	}
	m.fulfillments[f.ID] = f
	return nil
}

func (m *mockRepo) Get(ctx context.Context, id string) (*Fulfillment, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if f, ok := m.fulfillments[id]; ok {
		return f, nil
	}
	return nil, ErrNotFound
}

func (m *mockRepo) Update(ctx context.Context, f *Fulfillment) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.fulfillments[f.ID] = f
	return nil
}

func (m *mockRepo) ListByOrder(ctx context.Context, orderID string) ([]Fulfillment, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []Fulfillment
	for _, f := range m.fulfillments {
		if f.OrderID == orderID {
			result = append(result, *f)
		}
	}
	return result, nil
}

type mockOrderFetcher struct {
	order *OrderInfo
	err   error
}

func (m *mockOrderFetcher) Get(ctx context.Context, id string) (*OrderInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.order, nil
}

func TestCreateFulfillment(t *testing.T) {
	tests := []struct {
		name     string
		order    *OrderInfo
		existing []Fulfillment
		items    []Item
		wantErr  error
	}{
		{
			name: "successful creation",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 3},
			},
			wantErr: nil,
		},
		{
			name: "order not confirmed",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "pending",
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 1},
			},
			wantErr: ErrOrderNotConfirmed,
		},
		{
			name: "over fulfillment rejected",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 10},
			},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "partial then over fulfillment",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			existing: []Fulfillment{
				{
					ID:      "ful-001",
					OrderID: "ord-001",
					Items: []Item{
						{VariantID: "var-001", Quantity: 3},
					},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 3},
			},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "partial fulfillment allowed",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
					{VariantID: "var-002", Quantity: 3},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 3},
			},
			wantErr: nil,
		},
		{
			name: "non-existent variant",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			items: []Item{
				{VariantID: "var-999", Quantity: 1},
			},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "zero quantity",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: 0},
			},
			wantErr: ErrOverFulfillment,
		},
		{
			name: "negative quantity",
			order: &OrderInfo{
				ID:     "ord-001",
				Status: "confirmed",
				Lines: []OrderLine{
					{VariantID: "var-001", Quantity: 5},
				},
			},
			items: []Item{
				{VariantID: "var-001", Quantity: -1},
			},
			wantErr: ErrOverFulfillment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				fulfillments: make(map[string]*Fulfillment),
			}
			for i := range tt.existing {
				repo.fulfillments[tt.existing[i].ID] = &tt.existing[i]
			}

			orderFetcher := &mockOrderFetcher{
				order: tt.order,
			}

			svc := NewService(repo, orderFetcher)
			ctx := context.Background()

			f, err := svc.CreateFulfillment(ctx, "ord-001", tt.items)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("CreateFulfillment() error = %v, want %v", err, tt.wantErr)
				}
				if f != nil {
					t.Errorf("CreateFulfillment() should return nil on error")
				}
			} else {
				if err != nil {
					t.Errorf("CreateFulfillment() unexpected error = %v", err)
				}
				if f == nil {
					t.Errorf("CreateFulfillment() returned nil")
				}
				if f.Status != StatusPending {
					t.Errorf("CreateFulfillment() status = %v, want pending", f.Status)
				}
				if f.OrderID != "ord-001" {
					t.Errorf("CreateFulfillment() orderID = %v, want ord-001", f.OrderID)
				}
			}
		})
	}
}

func TestTransitionStatus(t *testing.T) {
	repo := &mockRepo{
		fulfillments: map[string]*Fulfillment{
			"ful-001": {
				ID:        "ful-001",
				OrderID:   "ord-001",
				Status:    StatusPending,
				Items:     []Item{{VariantID: "var-001", Quantity: 1}},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	orderFetcher := &mockOrderFetcher{}
	svc := NewService(repo, orderFetcher)
	ctx := context.Background()

	t.Run("valid transition", func(t *testing.T) {
		err := svc.TransitionStatus(ctx, "ful-001", StatusPicked)
		if err != nil {
			t.Errorf("TransitionStatus() error = %v", err)
		}

		f, _ := svc.Get(ctx, "ful-001")
		if f.Status != StatusPicked {
			t.Errorf("TransitionStatus() status = %v, want picked", f.Status)
		}
	})

	t.Run("invalid transition", func(t *testing.T) {
		repo.fulfillments["ful-001"].Status = StatusDelivered
		err := svc.TransitionStatus(ctx, "ful-001", StatusPending)
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("TransitionStatus() error = %v, want %v", err, ErrInvalidTransition)
		}
	})

	t.Run("fulfillment not found", func(t *testing.T) {
		err := svc.TransitionStatus(ctx, "nonexistent", StatusPicked)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("TransitionStatus() error = %v, want %v", err, ErrNotFound)
		}
	})

	t.Run("invalid status value", func(t *testing.T) {
		repo.fulfillments["ful-001"].Status = StatusPending
		err := svc.TransitionStatus(ctx, "ful-001", Status("invalid"))
		if !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("TransitionStatus() error = %v, want %v", err, ErrInvalidStatus)
		}
	})
}

func TestServiceAddTracking(t *testing.T) {
	repo := &mockRepo{
		fulfillments: map[string]*Fulfillment{
			"ful-001": {
				ID:        "ful-001",
				OrderID:   "ord-001",
				Status:    StatusShipped,
				Items:     []Item{{VariantID: "var-001", Quantity: 1}},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	orderFetcher := &mockOrderFetcher{}
	svc := NewService(repo, orderFetcher)
	ctx := context.Background()

	t.Run("valid tracking", func(t *testing.T) {
		tracking := Tracking{
			Carrier: "DHL",
			Number:  "123456",
			URL:     "https://track.dhl.com/123456",
		}

		err := svc.AddTracking(ctx, "ful-001", tracking)
		if err != nil {
			t.Errorf("AddTracking() error = %v", err)
		}

		f, _ := svc.Get(ctx, "ful-001")
		if f.Tracking == nil {
			t.Errorf("AddTracking() tracking is nil")
		}
		if f.Tracking.Carrier != tracking.Carrier {
			t.Errorf("AddTracking() carrier = %v, want %v", f.Tracking.Carrier, tracking.Carrier)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		tracking := Tracking{
			Carrier: "DHL",
			Number:  "789012",
			URL:     "http://track.dhl.com/789012",
		}

		err := svc.AddTracking(ctx, "ful-001", tracking)
		if !errors.Is(err, ErrInvalidTrackingURL) {
			t.Errorf("AddTracking() error = %v, want %v", err, ErrInvalidTrackingURL)
		}
	})

	t.Run("fulfillment not found", func(t *testing.T) {
		tracking := Tracking{
			Carrier: "DHL",
			Number:  "123456",
			URL:     "https://track.dhl.com/123456",
		}

		err := svc.AddTracking(ctx, "nonexistent", tracking)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("AddTracking() error = %v, want %v", err, ErrNotFound)
		}
	})
}

func TestListByOrder(t *testing.T) {
	repo := &mockRepo{
		fulfillments: map[string]*Fulfillment{
			"ful-001": {
				ID:        "ful-001",
				OrderID:   "ord-001",
				Status:    StatusPending,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			"ful-002": {
				ID:        "ful-002",
				OrderID:   "ord-001",
				Status:    StatusShipped,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			"ful-003": {
				ID:        "ful-003",
				OrderID:   "ord-002",
				Status:    StatusPending,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	orderFetcher := &mockOrderFetcher{}
	svc := NewService(repo, orderFetcher)
	ctx := context.Background()

	t.Run("list fulfillments for order", func(t *testing.T) {
		fulfillments, err := svc.ListByOrder(ctx, "ord-001")
		if err != nil {
			t.Errorf("ListByOrder() error = %v", err)
		}
		if len(fulfillments) != 2 {
			t.Errorf("ListByOrder() count = %d, want 2", len(fulfillments))
		}
	})

	t.Run("empty list for order", func(t *testing.T) {
		fulfillments, err := svc.ListByOrder(ctx, "ord-999")
		if err != nil {
			t.Errorf("ListByOrder() error = %v", err)
		}
		if len(fulfillments) != 0 {
			t.Errorf("ListByOrder() count = %d, want 0", len(fulfillments))
		}
	})
}
