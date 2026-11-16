package paystack

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
)

// PostgresEventStore persists processed Paystack webhook events in PostgreSQL
// so deduplication survives server restarts.
type PostgresEventStore struct {
	db *sql.DB
}

func NewPostgresEventStore(db *sql.DB) *PostgresEventStore {
	return &PostgresEventStore{db: db}
}

func (s *PostgresEventStore) GetEvent(eventID string) (*ProcessedEvent, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		ev    ProcessedEvent
		state string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT event_id, reference, state, event_ts
		FROM paystack_webhook_events
		WHERE event_id = $1
	`, eventID).Scan(&ev.EventID, &ev.Reference, &state, &ev.Timestamp)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return nil, false
	}
	ev.State = payment.State(state)
	return &ev, true
}

func (s *PostgresEventStore) SaveEvent(event *ProcessedEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO paystack_webhook_events (event_id, reference, state, event_ts)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.Reference, string(event.State), event.Timestamp)
	return err
}
