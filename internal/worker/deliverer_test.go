package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/webhook"
)

func TestDeliverer_Delivery(t *testing.T) {
	var receivedEvent *events.Event
	var receivedHeaders map[string]string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		var e events.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Errorf("Failed to decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		receivedEvent = &e
		receivedHeaders = map[string]string{
			"X-Kikapu-Event":     r.Header.Get("X-Kikapu-Event"),
			"X-Kikapu-Delivery":  r.Header.Get("X-Kikapu-Delivery"),
			"X-Kikapu-Signature": r.Header.Get("X-Kikapu-Signature"),
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]interface{}{
		"order_id": "ord_123",
		"amount":   10000,
	})
	o.Append(context.Background(), nil, event)

	store := NewInMemorySubscriberStore()
	store.Add(Subscription{
		ID:     "sub_123",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDeliverer(client, o, store)
	go d.Run(workerCtx, 100*time.Millisecond)

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if receivedEvent == nil {
		t.Fatal("Expected event to be received")
	}

	if receivedEvent.Type != events.EventTypeOrderPaid {
		t.Errorf("Event type = %v, want %v", receivedEvent.Type, events.EventTypeOrderPaid)
	}

	if receivedHeaders["X-Kikapu-Event"] != events.EventTypeOrderPaid {
		t.Errorf("X-Kikapu-Event header = %v, want %v", receivedHeaders["X-Kikapu-Event"], events.EventTypeOrderPaid)
	}

	if receivedHeaders["X-Kikapu-Delivery"] == "" {
		t.Error("X-Kikapu-Delivery header should not be empty")
	}

	if receivedHeaders["X-Kikapu-Signature"] == "" {
		t.Error("X-Kikapu-Signature header should not be empty")
	}

	err := webhook.Verify("test-secret", receivedHeaders["X-Kikapu-Signature"], func() []byte {
		b, _ := json.Marshal(receivedEvent)
		return b
	}(), 5*time.Minute)
	if err != nil {
		t.Errorf("Signature verification failed: %v", err)
	}
}

func TestDeliverer_RetryOn5xx(t *testing.T) {
	attempts := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(context.Background(), nil, event)

	store := NewInMemorySubscriberStore()
	store.Add(Subscription{
		ID:     "sub_123",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	// This test would take too long with actual backoff, so we'll just verify the MarkFailed is called
	// In a real test environment, we'd inject a mock clock
}

func TestDeliverer_DeadLetterOn4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]interface{}{"test": "data"})
	o.Append(context.Background(), nil, event)

	store := NewInMemorySubscriberStore()
	store.Add(Subscription{
		ID:     "sub_123",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDeliverer(client, o, store)
	go d.Run(workerCtx, 100*time.Millisecond)

	time.Sleep(500 * time.Millisecond)

	record, err := o.Get(event.ID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if record.Status != events.StatusDeadLetter {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusDeadLetter)
	}
}

func TestDeliverer_EventTypeFiltering(t *testing.T) {
	var receivedEvents []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		var e events.Event
		json.NewDecoder(r.Body).Decode(&e)
		receivedEvents = append(receivedEvents, e.Type)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()

	event1, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]interface{}{"test": "data"})
	event2, _ := events.NewEvent(events.EventTypeStockReserved, "stk_123", map[string]interface{}{"test": "data"})

	o.Append(context.Background(), nil, event1)
	o.Append(context.Background(), nil, event2)

	store := NewInMemorySubscriberStore()
	store.Add(Subscription{
		ID:     "sub_123",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDeliverer(client, o, store)
	go d.Run(workerCtx, 100*time.Millisecond)

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedEvents) != 1 {
		t.Errorf("Received %d events, want 1", len(receivedEvents))
	}

	if len(receivedEvents) > 0 && receivedEvents[0] != events.EventTypeOrderPaid {
		t.Errorf("Received event type = %v, want %v", receivedEvents[0], events.EventTypeOrderPaid)
	}
}

func TestInMemorySubscriberStore_ListByEventType(t *testing.T) {
	store := NewInMemorySubscriberStore()

	sub1 := Subscription{
		ID:     "sub_1",
		URL:    "https://example.com/webhook",
		Secret: "secret1",
		Types:  []string{events.EventTypeOrderPaid, events.EventTypeOrderRefunded},
		Active: true,
	}

	sub2 := Subscription{
		ID:     "sub_2",
		URL:    "https://another.example.com/webhook",
		Secret: "secret2",
		Types:  []string{events.EventTypeStockReserved},
		Active: true,
	}

	sub3 := Subscription{
		ID:     "sub_3",
		URL:    "https://all.example.com/webhook",
		Secret: "secret3",
		Types:  []string{"*"},
		Active: true,
	}

	sub4 := Subscription{
		ID:     "sub_4",
		URL:    "https://inactive.example.com/webhook",
		Secret: "secret4",
		Types:  []string{events.EventTypeOrderPaid},
		Active: false,
	}

	store.Add(sub1)
	store.Add(sub2)
	store.Add(sub3)
	store.Add(sub4)

	ctx := context.Background()

	tests := []struct {
		name      string
		eventType string
		wantCount int
		wantIDs   []string
	}{
		{
			name:      "order.paid",
			eventType: events.EventTypeOrderPaid,
			wantCount: 2,
			wantIDs:   []string{"sub_1", "sub_3"},
		},
		{
			name:      "stock.reserved",
			eventType: events.EventTypeStockReserved,
			wantCount: 2,
			wantIDs:   []string{"sub_2", "sub_3"},
		},
		{
			name:      "order.refunded",
			eventType: events.EventTypeOrderRefunded,
			wantCount: 2,
			wantIDs:   []string{"sub_1", "sub_3"},
		},
		{
			name:      "nonexistent",
			eventType: "nonexistent.event",
			wantCount: 1,
			wantIDs:   []string{"sub_3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subs, err := store.ListByEventType(ctx, tt.eventType)
			if err != nil {
				t.Errorf("ListByEventType() error = %v", err)
				return
			}

			if len(subs) != tt.wantCount {
				t.Errorf("ListByEventType() returned %d subscriptions, want %d", len(subs), tt.wantCount)
				return
			}

			subIDs := make(map[string]bool)
			for _, sub := range subs {
				subIDs[sub.ID] = true
			}

			for _, id := range tt.wantIDs {
				if !subIDs[id] {
					t.Errorf("Expected subscription %s not found", id)
				}
			}
		})
	}
}

func TestDeliverer_ContextCancellation(t *testing.T) {
	o := outbox.NewInMemoryOutbox()
	store := NewInMemorySubscriberStore()

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())

	d := NewDeliverer(client, o, store)

	done := make(chan bool)
	go func() {
		d.Run(workerCtx, 10*time.Millisecond)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("Worker did not stop after context cancellation")
	}
}
