package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/webhook"
)

type Subscription struct {
	ID     string
	URL    string
	Secret string
	Types  []string
	Active bool
}

type SubscriberStore interface {
	List(ctx context.Context) ([]Subscription, error)
	ListByEventType(ctx context.Context, eventType string) ([]Subscription, error)
}

type Deliverer struct {
	client  *http.Client
	outbox  events.Outbox
	store   SubscriberStore
	signer  func(secret string, ts time.Time, body []byte) string
	backoff func(attempt int) time.Time
}

func NewDeliverer(client *http.Client, outbox events.Outbox, store SubscriberStore) *Deliverer {
	return &Deliverer{
		client:  client,
		outbox:  outbox,
		store:   store,
		signer:  webhook.Sign,
		backoff: defaultBackoff,
	}
}

func defaultBackoff(attempt int) time.Time {
	backoffs := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		30 * time.Second,
		5 * time.Minute,
	}

	if attempt >= len(backoffs) {
		return time.Now().UTC().Add(backoffs[len(backoffs)-1])
	}

	return time.Now().UTC().Add(backoffs[attempt])
}

func (d *Deliverer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.ProcessBatch(ctx)
		}
	}
}

func (d *Deliverer) ProcessBatch(ctx context.Context) {
	records, err := d.outbox.Claim(ctx, 10)
	if err != nil {
		return
	}

	for _, record := range records {
		subscribers, err := d.store.ListByEventType(ctx, record.Event.Type)
		if err != nil {
			continue
		}

		for _, sub := range subscribers {
			if !sub.Active {
				continue
			}

			for _, et := range sub.Types {
				if et == record.Event.Type || et == "*" {
					d.deliver(ctx, record, sub)
					break
				}
			}
		}
	}
}

func (d *Deliverer) deliver(ctx context.Context, record events.Record, sub Subscription) {
	body, err := json.Marshal(record.Event)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sub.URL, bytes.NewReader(body))
	if err != nil {
		return
	}

	ts := time.Now().UTC()
	signature := d.signer(sub.Secret, ts, body)
	sigHeader := webhook.BuildSignatureHeader(ts, signature)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kikapu-Event", record.Event.Type)
	req.Header.Set("X-Kikapu-Delivery", record.ID)
	req.Header.Set("X-Kikapu-Signature", sigHeader)

	resp, err := d.client.Do(req)
	if err != nil {
		d.outbox.MarkFailed(ctx, record.ID, err, d.backoff(record.Attempts))
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		d.outbox.MarkSent(ctx, record.ID)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		d.outbox.MarkFailed(ctx, record.ID, fmt.Errorf("4xx response: %d", resp.StatusCode), time.Time{})
	default:
		d.outbox.MarkFailed(ctx, record.ID, fmt.Errorf("5xx response: %d", resp.StatusCode), d.backoff(record.Attempts))
	}
}

type InMemorySubscriberStore struct {
	subscriptions []Subscription
}

func NewInMemorySubscriberStore() *InMemorySubscriberStore {
	return &InMemorySubscriberStore{
		subscriptions: make([]Subscription, 0),
	}
}

func (s *InMemorySubscriberStore) Add(sub Subscription) {
	s.subscriptions = append(s.subscriptions, sub)
}

func (s *InMemorySubscriberStore) List(ctx context.Context) ([]Subscription, error) {
	result := make([]Subscription, len(s.subscriptions))
	copy(result, s.subscriptions)
	return result, nil
}

func (s *InMemorySubscriberStore) ListByEventType(ctx context.Context, eventType string) ([]Subscription, error) {
	var result []Subscription
	for _, sub := range s.subscriptions {
		if !sub.Active {
			continue
		}
		for _, t := range sub.Types {
			if t == eventType || t == "*" {
				result = append(result, sub)
				break
			}
		}
	}
	return result, nil
}
