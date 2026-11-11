package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
)

func TestInMemoryOutbox_Append(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, err := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	err = o.Append(ctx, nil, event)
	if err != nil {
		t.Errorf("Append() error = %v", err)
	}

	if o.Pending() != 1 {
		t.Errorf("Pending() = %v, want 1", o.Pending())
	}
}

func TestInMemoryOutbox_Claim(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event1, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	event2, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_456", map[string]interface{}{"test": "data"})

	o.Append(ctx, nil, event1)
	o.Append(ctx, nil, event2)

	records, err := o.Claim(ctx, 1)
	if err != nil {
		t.Errorf("Claim() error = %v", err)
	}

	if len(records) != 1 {
		t.Errorf("Claim() returned %d records, want 1", len(records))
	}

	records2, err := o.Claim(ctx, 10)
	if err != nil {
		t.Errorf("Claim() error = %v", err)
	}

	if len(records2) != 1 {
		t.Errorf("Claim() returned %d records, want 1 (one already claimed)", len(records2))
	}
}

func TestInMemoryOutbox_MarkSent(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(ctx, nil, event)

	records, _ := o.Claim(ctx, 1)
	if len(records) == 0 {
		t.Fatal("Expected to claim at least one record")
	}

	err := o.MarkSent(ctx, records[0].ID)
	if err != nil {
		t.Errorf("MarkSent() error = %v", err)
	}

	record, err := o.Get(event.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if record.Status != events.StatusSent {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusSent)
	}

	if o.Pending() != 0 {
		t.Errorf("Pending() = %v, want 0", o.Pending())
	}
}

func TestInMemoryOutbox_MarkFailed(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(ctx, nil, event)

	records, _ := o.Claim(ctx, 1)
	if len(records) == 0 {
		t.Fatal("Expected to claim at least one record")
	}

	nextAttempt := time.Now().Add(5 * time.Second)
	err := o.MarkFailed(ctx, records[0].ID, errors.New("delivery failed"), nextAttempt)
	if err != nil {
		t.Errorf("MarkFailed() error = %v", err)
	}

	record, err := o.Get(event.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if record.Status != events.StatusPending {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusPending)
	}

	if record.Attempts != 1 {
		t.Errorf("Attempts = %v, want 1", record.Attempts)
	}

	if record.LastError == "" {
		t.Error("LastError should not be empty")
	}

	if record.NextAttempt.IsZero() {
		t.Error("NextAttempt should be set")
	}
}

func TestInMemoryOutbox_DeadLetter(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(ctx, nil, event)

	for i := 0; i < 8; i++ {
		records, _ := o.Claim(ctx, 1)
		if len(records) == 0 {
			t.Fatalf("Expected to claim record on attempt %d", i+1)
		}

		nextAttempt := time.Now().UTC()
		o.MarkFailed(ctx, records[0].ID, errors.New("delivery failed"), nextAttempt)
	}

	record, err := o.Get(event.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if record.Status != events.StatusDeadLetter {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusDeadLetter)
	}

	if record.Attempts != 8 {
		t.Errorf("Attempts = %v, want 8", record.Attempts)
	}
}

func TestOutboxAtomic(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})

	failingAppend := func(ctx context.Context, tx events.Tx, e events.Event) error {
		return errors.New("simulated write failure")
	}

	err := failingAppend(ctx, nil, event)
	if err == nil {
		t.Error("Expected error from failing append")
	}

	records, err := o.Claim(ctx, 10)
	if err != nil {
		t.Errorf("Claim() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Claim() returned %d records, want 0 (atomicity test)", len(records))
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{"first attempt", 0, 1 * time.Second},
		{"second attempt", 1, 5 * time.Second},
		{"third attempt", 2, 30 * time.Second},
		{"fourth attempt", 3, 5 * time.Minute},
		{"fifth attempt (max)", 4, 5 * time.Minute},
		{"eighth attempt (max)", 7, 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextAttempt := NextBackoff(tt.attempt)
			expected := time.Now().UTC().Add(tt.expected)

			diff := nextAttempt.Sub(expected)
			if diff < 0 {
				diff = -diff
			}

			if diff > 100*time.Millisecond {
				t.Errorf("NextBackoff() = %v, expected approximately %v", nextAttempt, expected)
			}
		})
	}
}

func TestInMemoryOutbox_ClaimExclusivity(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event1, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	event2, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_456", map[string]interface{}{"test": "data"})

	o.Append(ctx, nil, event1)
	o.Append(ctx, nil, event2)

	records1, _ := o.Claim(ctx, 10)
	records2, _ := o.Claim(ctx, 10)

	claimedIDs := make(map[string]bool)
	for _, r := range records1 {
		claimedIDs[r.ID] = true
	}
	for _, r := range records2 {
		if claimedIDs[r.ID] {
			t.Errorf("Record %s claimed by both workers", r.ID)
		}
	}
}

func TestClaimWithFutureNextAttempt(t *testing.T) {
	o := NewInMemoryOutbox()
	ctx := context.Background()

	event, _ := events.NewEvent(events.EventTypeOrderCreated, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(ctx, nil, event)

	records, _ := o.Claim(ctx, 1)
	o.MarkFailed(ctx, records[0].ID, errors.New("failed"), time.Now().Add(1*time.Hour))

	records2, _ := o.Claim(ctx, 10)
	if len(records2) != 0 {
		t.Errorf("Claim() should not return records with future NextAttempt, got %d", len(records2))
	}
}
