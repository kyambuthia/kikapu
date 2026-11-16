package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/webhook"
)

const maxWebhookResponseBytes = 64 << 10 // 64 KiB

var errDisallowedWebhookTarget = errors.New("webhook target address is not allowed")

// NewSafeWebhookClient returns an HTTP client whose dialer blocks connections
// to loopback, private, link-local (including cloud metadata), and multicast IPs.
// Checking at dial time prevents DNS rebinding SSRF.
func NewSafeWebhookClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if isBlockedAddr(addr) {
				return fmt.Errorf("%w: %s", errDisallowedWebhookTarget, addr.String())
			}
			return nil
		},
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        64,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

func isBlockedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified()
}

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

// EventConsumer handles claimed outbox events in-process (e.g. notifications).
type EventConsumer interface {
	HandleEvent(ctx context.Context, event events.Event) error
}

type Deliverer struct {
	client    *http.Client
	outbox    events.Outbox
	store     SubscriberStore
	consumers []EventConsumer
	signer    func(secret string, ts time.Time, body []byte) string
	backoff   func(attempt int) time.Time
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

// AddConsumer registers an in-process event consumer invoked on each claimed event.
func (d *Deliverer) AddConsumer(c EventConsumer) {
	if c != nil {
		d.consumers = append(d.consumers, c)
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
		var consumerErr error
		for _, c := range d.consumers {
			if err := c.HandleEvent(ctx, record.Event); err != nil {
				consumerErr = err
				break
			}
		}
		if consumerErr != nil {
			_ = d.outbox.MarkFailed(ctx, record.ID, consumerErr, d.backoff(record.Attempts))
			continue
		}

		subscribers, err := d.store.ListByEventType(ctx, record.Event.Type)
		if err != nil {
			// Release the claimed record back for retry instead of leaving it stuck.
			_ = d.outbox.MarkFailed(ctx, record.ID, err, d.backoff(record.Attempts))
			continue
		}

		var (
			matched   int
			firstErr  error
			retryable bool
		)
		for _, sub := range subscribers {
			if !sub.Active || !subMatchesEvent(sub.Types, record.Event.Type) {
				continue
			}
			matched++
			canRetry, err := d.deliver(ctx, record, sub)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if canRetry {
					retryable = true
				}
			}
		}

		if firstErr != nil {
			nextAttempt := time.Time{}
			if retryable {
				nextAttempt = d.backoff(record.Attempts)
			}
			_ = d.outbox.MarkFailed(ctx, record.ID, firstErr, nextAttempt)
			continue
		}

		if matched > 0 || len(d.consumers) > 0 {
			_ = d.outbox.MarkSent(ctx, record.ID)
		}
	}
}

func subMatchesEvent(types []string, eventType string) bool {
	for _, et := range types {
		if et == eventType || et == "*" {
			return true
		}
	}
	return false
}

func (d *Deliverer) deliver(ctx context.Context, record events.Record, sub Subscription) (bool, error) {
	parsedURL, err := url.Parse(sub.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return false, fmt.Errorf("invalid webhook URL")
	}

	body, err := json.Marshal(record.Event)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(body))
	if err != nil {
		return false, err
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
		return true, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return false, fmt.Errorf("4xx response: %d", resp.StatusCode)
	default:
		return true, fmt.Errorf("5xx response: %d", resp.StatusCode)
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
