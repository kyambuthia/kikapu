package events

import (
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		aggregateID string
		data        interface{}
		wantErr     bool
	}{
		{
			name:        "valid event",
			eventType:   EventTypeOrderCreated,
			aggregateID: "ord_123",
			data: map[string]interface{}{
				"order_id": "ord_123",
				"status":   "pending",
			},
			wantErr: false,
		},
		{
			name:        "empty event type",
			eventType:   "",
			aggregateID: "ord_123",
			data:        map[string]interface{}{},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := NewEvent(tt.eventType, tt.aggregateID, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if event.ID == "" {
					t.Error("Event ID should not be empty")
				}
				if event.Type != tt.eventType {
					t.Errorf("Event type = %v, want %v", event.Type, tt.eventType)
				}
				if event.AggregateID != tt.aggregateID {
					t.Errorf("Aggregate ID = %v, want %v", event.AggregateID, tt.aggregateID)
				}
				if event.Version != 1 {
					t.Errorf("Event version should be 1, got %v", event.Version)
				}
				if event.OccurredAt.IsZero() {
					t.Error("OccurredAt should be set")
				}
				if len(event.Data) == 0 {
					t.Error("Event data should not be empty")
				}
			}
		})
	}
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	if id1 == "" {
		t.Error("Event ID should not be empty")
	}

	if id2 == "" {
		t.Error("Event ID should not be empty")
	}

	if id1 == id2 {
		t.Error("Two generated IDs should be different")
	}

	if len(id1) < 10 {
		t.Errorf("Event ID too short: %s", id1)
	}
}

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name      string
		constant  string
		wantValue string
	}{
		{"order.created", EventTypeOrderCreated, "order.created"},
		{"order.paid", EventTypeOrderPaid, "order.paid"},
		{"order.fulfilled", EventTypeOrderFulfilled, "order.fulfilled"},
		{"order.refunded", EventTypeOrderRefunded, "order.refunded"},
		{"stock.reserved", EventTypeStockReserved, "stock.reserved"},
		{"stock.released", EventTypeStockReleased, "stock.released"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.wantValue {
				t.Errorf("Constant %s = %v, want %v", tt.name, tt.constant, tt.wantValue)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %v, want pending", StatusPending)
	}
	if StatusSent != "sent" {
		t.Errorf("StatusSent = %v, want sent", StatusSent)
	}
	if StatusDeadLetter != "dead_letter" {
		t.Errorf("StatusDeadLetter = %v, want dead_letter", StatusDeadLetter)
	}
}

func TestEventTimestamp(t *testing.T) {
	before := time.Now().UTC()
	event, err := NewEvent(EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewEvent() error = %v", err)
	}

	if event.OccurredAt.Before(before) || event.OccurredAt.After(after) {
		t.Errorf("Event timestamp %v not in expected range [%v, %v]", event.OccurredAt, before, after)
	}
}
