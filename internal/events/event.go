package events

import (
	"context"
	"encoding/json"
	"time"
)

type Event struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Version     int             `json:"version"`
	AggregateID string          `json:"aggregate_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Data        json.RawMessage `json:"data"`
}

type Record struct {
	ID          string    `json:"id"`
	Event       Event     `json:"event"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const (
	StatusPending    = "pending"
	StatusSent       = "sent"
	StatusDeadLetter = "dead_letter"
)

const (
	EventTypeOrderCreated   = "order.created"
	EventTypeOrderPaid      = "order.paid"
	EventTypeOrderFulfilled = "order.fulfilled"
	EventTypeOrderRefunded  = "order.refunded"
	EventTypeStockReserved  = "stock.reserved"
	EventTypeStockReleased  = "stock.released"
)

type Tx interface{}

type Outbox interface {
	Append(ctx context.Context, tx Tx, e Event) error
	Claim(ctx context.Context, limit int) ([]Record, error)
	MarkSent(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, err error, nextAttempt time.Time) error
}

func NewEvent(eventType, aggregateID string, data interface{}) (Event, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}

	return Event{
		ID:          generateEventID(),
		Type:        eventType,
		Version:     1,
		AggregateID: aggregateID,
		OccurredAt:  time.Now().UTC(),
		Data:        json.RawMessage(dataBytes),
	}, nil
}

func generateEventID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return "evt_" + string(b)
}
