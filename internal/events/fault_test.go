package events_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/worker"
)

type FakeClock struct {
	current time.Time
}

func (f *FakeClock) Now() time.Time {
	return f.current
}

func (f *FakeClock) Add(d time.Duration) {
	f.current = f.current.Add(d)
}

type CountingHandler struct {
	mu       sync.Mutex
	count    int
	attempts []int
}

func (h *CountingHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count = 0
	h.attempts = nil
}

func (h *CountingHandler) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func (h *CountingHandler) Attempts() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]int, len(h.attempts))
	copy(result, h.attempts)
	return result
}

type FailThenSucceedHandler struct {
	*CountingHandler
	failCount int
}

func NewFailThenSucceedHandler(failBeforeSuccess int) *FailThenSucceedHandler {
	return &FailThenSucceedHandler{
		CountingHandler: &CountingHandler{
			attempts: make([]int, 0),
		},
		failCount: failBeforeSuccess,
	}
}

func (h *FailThenSucceedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.count++
	h.attempts = append(h.attempts, h.count)
	count := h.count
	h.mu.Unlock()

	if count <= h.failCount {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func TestWebhookDelivery_RetriesOn5xx_ThenSucceeds(t *testing.T) {
	handler := NewFailThenSucceedHandler(2)
	server := httptest.NewServer(handler)
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, err := events.NewEvent(events.EventTypeOrderPaid, "ord_123", map[string]interface{}{
		"order_id": "ord_123",
		"amount":   10000,
	})
	if err != nil {
		t.Fatalf("NewEvent error: %v", err)
	}

	ctx := context.Background()
	if err := o.Append(ctx, nil, event); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_123",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(7 * time.Second)

	record, err := o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Status != events.StatusSent {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusSent)
	}

	if record.Attempts < 2 {
		t.Errorf("Expected at least 2 attempts, got %d", record.Attempts)
	}

	attempts := handler.Attempts()
	if len(attempts) < 3 {
		t.Errorf("Webhook handler called %d times, want at least 3", len(attempts))
	}
}

type SlowHandler struct {
	mu           sync.Mutex
	requestCount int
	delay        time.Duration
}

func NewSlowHandler(delay time.Duration) *SlowHandler {
	return &SlowHandler{delay: delay}
}

func (h *SlowHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.requestCount++
	h.mu.Unlock()

	time.Sleep(h.delay)

	w.WriteHeader(http.StatusOK)
}

func (h *SlowHandler) RequestCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requestCount
}

func TestWebhookDelivery_ExactlyOnceDelivery(t *testing.T) {
	var receivedCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, err := events.NewEvent(events.EventTypeOrderPaid, "ord_456", map[string]interface{}{
		"order_id": "ord_456",
	})
	if err != nil {
		t.Fatalf("NewEvent error: %v", err)
	}

	ctx := context.Background()
	if err := o.Append(ctx, nil, event); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_456",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	record, err := o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Status != events.StatusSent {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusSent)
	}

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("Webhook received %d times, want exactly 1", count)
	}
}

func TestOutbox_AppendSucceeds_DeliveryNeverRuns(t *testing.T) {
	var receivedCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, err := events.NewEvent(events.EventTypeOrderCreated, "ord_789", map[string]interface{}{
		"order_id": "ord_789",
	})
	if err != nil {
		t.Fatalf("NewEvent error: %v", err)
	}

	ctx := context.Background()
	if err := o.Append(ctx, nil, event); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	record, err := o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Status != events.StatusPending {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusPending)
	}

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_789",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderCreated},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	record, err = o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Status != events.StatusSent {
		t.Errorf("After delivery: Status = %v, want %v", record.Status, events.StatusSent)
	}

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("Webhook called %d times, want 1", count)
	}
}

func TestWebhookDelivery_4xxGoesToDeadLetter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, err := events.NewEvent(events.EventTypeOrderPaid, "ord_bad", map[string]interface{}{
		"order_id": "ord_bad",
	})
	if err != nil {
		t.Fatalf("NewEvent error: %v", err)
	}

	ctx := context.Background()
	if err := o.Append(ctx, nil, event); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_bad",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	record, err := o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Status != events.StatusDeadLetter {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusDeadLetter)
	}
}

func TestWebhookDelivery_RecordsAttempts(t *testing.T) {
	attemptCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		mu.Unlock()

		if currentAttempt < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, err := events.NewEvent(events.EventTypeOrderPaid, "ord_attempts", map[string]interface{}{
		"order_id": "ord_attempts",
	})
	if err != nil {
		t.Fatalf("NewEvent error: %v", err)
	}

	ctx := context.Background()
	if err := o.Append(ctx, nil, event); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_attempts",
		URL:    server.URL,
		Secret: "test-secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(7 * time.Second)

	record, err := o.Get(event.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	if record.Attempts < 1 {
		t.Errorf("Recorded %d attempts, want at least 1", record.Attempts)
	}

	if record.Status != events.StatusSent {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusSent)
	}

	mu.Lock()
	finalAttemptCount := attemptCount
	mu.Unlock()

	if finalAttemptCount < 2 {
		t.Errorf("Webhook called %d times, want at least 2", finalAttemptCount)
	}
}

type FailingOutbox struct {
	mu                 sync.Mutex
	appended           []events.Event
	markSentShouldFail bool
	markSentFailCount  int
	markSentCallCount  int
}

func NewFailingOutbox() *FailingOutbox {
	return &FailingOutbox{
		appended: make([]events.Event, 0),
	}
}

func (o *FailingOutbox) Append(ctx context.Context, tx events.Tx, e events.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.appended = append(o.appended, e)
	return nil
}

func (o *FailingOutbox) Claim(ctx context.Context, limit int) ([]events.Record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var records []events.Record
	for _, e := range o.appended {
		records = append(records, events.Record{
			ID:       e.ID,
			Event:    e,
			Status:   events.StatusPending,
			Attempts: 0,
		})
	}
	return records, nil
}

func (o *FailingOutbox) MarkSent(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.markSentCallCount++

	if o.markSentShouldFail && o.markSentCallCount <= o.markSentFailCount {
		return errors.New("mark sent failed")
	}

	return nil
}

func (o *FailingOutbox) MarkFailed(ctx context.Context, id string, err error, nextAttempt time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return nil
}

func (o *FailingOutbox) SetMarkSentFail(fail bool, count int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.markSentShouldFail = fail
	o.markSentFailCount = count
}

type SpyDeliverer struct {
	*worker.Deliverer
	mu           sync.Mutex
	processCount int
}

func NewSpyDeliverer(client *http.Client, outbox events.Outbox, store worker.SubscriberStore) *SpyDeliverer {
	return &SpyDeliverer{
		Deliverer: worker.NewDeliverer(client, outbox, store),
	}
}

func (d *SpyDeliverer) ProcessBatch(ctx context.Context) {
	d.mu.Lock()
	d.processCount++
	d.mu.Unlock()

	d.Deliverer.ProcessBatch(ctx)
}

func (d *SpyDeliverer) ProcessCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.processCount
}

func TestDeliveryRecordsAttempts(t *testing.T) {
	attemptCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		mu.Unlock()

		if currentAttempt < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	o := outbox.NewInMemoryOutbox()
	event, _ := events.NewEvent(events.EventTypeOrderPaid, "ord_test", map[string]interface{}{"test": "data"})

	ctx := context.Background()
	o.Append(ctx, nil, event)

	store := worker.NewInMemorySubscriberStore()
	store.Add(worker.Subscription{
		ID:     "sub_test",
		URL:    server.URL,
		Secret: "secret",
		Types:  []string{events.EventTypeOrderPaid},
		Active: true,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := worker.NewDeliverer(client, o, store)
	go d.Run(workerCtx, 50*time.Millisecond)

	time.Sleep(7 * time.Second)

	record, _ := o.Get(event.ID)
	if record.Attempts < 1 {
		t.Errorf("Expected at least 1 attempt, got %d", record.Attempts)
	}

	if record.Status != events.StatusSent {
		t.Errorf("Status = %v, want %v", record.Status, events.StatusSent)
	}

	mu.Lock()
	finalAttemptCount := attemptCount
	mu.Unlock()

	if finalAttemptCount < 2 {
		t.Errorf("Webhook called %d times, want at least 2", finalAttemptCount)
	}
}
