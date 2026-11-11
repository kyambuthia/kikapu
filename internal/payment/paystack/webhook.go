package paystack

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

var (
	ErrInvalidSignature = errors.New("invalid signature")
	ErrMissingSignature = errors.New("missing signature")
	ErrInvalidPayload   = errors.New("invalid payload")
)

type WebhookEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type TransactionData struct {
	ID        int64  `json:"id"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	PaidAt    string `json:"paid_at"`
	CreatedAt string `json:"created_at"`
	Channel   string `json:"channel"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
}

type WebhookPayload struct {
	Event string          `json:"event"`
	Data  TransactionData `json:"data"`
}

type ProcessedEvent struct {
	EventID   string
	Reference string
	State     payment.State
	Timestamp int64
}

type EventStore interface {
	GetEvent(eventID string) (*ProcessedEvent, bool)
	SaveEvent(event *ProcessedEvent) error
}

type InMemoryEventStore struct {
	mu     sync.RWMutex
	events map[string]*ProcessedEvent
}

func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		events: make(map[string]*ProcessedEvent),
	}
}

func (s *InMemoryEventStore) GetEvent(eventID string) (*ProcessedEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, ok := s.events[eventID]
	return event, ok
}

func (s *InMemoryEventStore) SaveEvent(event *ProcessedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.EventID] = event
	return nil
}

type WebhookHandler struct {
	client     *Client
	eventStore EventStore
	logger     *slog.Logger
}

func NewWebhookHandler(client *Client, eventStore EventStore, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		client:     client,
		eventStore: eventStore,
		logger:     logger,
	}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	signature := r.Header.Get("X-Paystack-Signature")
	if signature == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "missing_signature", "missing X-Paystack-Signature header", httpx.RequestID(r))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_error", "failed to read request body", httpx.RequestID(r))
		return
	}
	defer r.Body.Close()

	if !h.verifySignature(body, signature, h.client.secretKey) {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid_signature", "invalid webhook signature", httpx.RequestID(r))
		return
	}

	var event WebhookPayload
	if err := json.Unmarshal(body, &event); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_payload", "failed to parse webhook payload", httpx.RequestID(r))
		return
	}

	if err := h.processEvent(r, &event); err != nil {
		if errors.Is(err, ErrInvalidPayload) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_event", err.Error(), httpx.RequestID(r))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) verifySignature(body []byte, signature, secretKey string) bool {
	expectedMAC := hmacSHA512(body, secretKey)
	expectedSig := hex.EncodeToString(expectedMAC)

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func hmacSHA512(data []byte, key string) []byte {
	h := hmac.New(sha512.New, []byte(key))
	h.Write(data)
	return h.Sum(nil)
}

func (h *WebhookHandler) processEvent(r *http.Request, event *WebhookPayload) error {
	requestID := httpx.RequestID(r)

	eventID := event.Data.ID
	if eventID == 0 {
		return ErrInvalidPayload
	}

	eventIDStr := int64ToString(eventID)

	if _, ok := h.eventStore.GetEvent(eventIDStr); ok {
		h.logger.Info("duplicate webhook event ignored",
			"event_id", eventID,
			"reference", event.Data.Reference,
			"request_id", requestID,
		)
		return nil
	}

	if _, ok := h.eventStore.GetEvent("ref_" + event.Data.Reference); ok {
		h.logger.Info("duplicate webhook event by reference ignored",
			"reference", event.Data.Reference,
			"event_id", eventID,
			"request_id", requestID,
		)
		return nil
	}

	switch event.Event {
	case "charge.success":
		h.handleChargeSuccess(event, requestID)
	default:
		h.logger.Info("ignoring unknown webhook event type",
			"event_type", event.Event,
			"request_id", requestID,
		)
	}

	return nil
}

func (h *WebhookHandler) handleChargeSuccess(event *WebhookPayload, requestID string) {
	state := mapPaystackStatus(event.Data.Status)

	processed := &ProcessedEvent{
		EventID:   int64ToString(event.Data.ID),
		Reference: event.Data.Reference,
		State:     state,
		Timestamp: event.Data.ID,
	}

	h.eventStore.SaveEvent(processed)

	refKey := "ref_" + event.Data.Reference
	h.eventStore.SaveEvent(&ProcessedEvent{
		EventID:   refKey,
		Reference: event.Data.Reference,
		State:     state,
		Timestamp: event.Data.ID,
	})

	h.logger.Info("charge success webhook processed",
		"reference", event.Data.Reference,
		"amount", event.Data.Amount,
		"currency", event.Data.Currency,
		"status", event.Data.Status,
		"state", state,
		"event_id", event.Data.ID,
		"request_id", requestID,
	)
}

func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	var negative bool
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
