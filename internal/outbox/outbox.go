package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
)

var ErrEventNotFound = errors.New("event not found")

type InMemoryOutbox struct {
	mu      sync.Mutex
	records map[string]*events.Record
	claimed map[string]time.Time
}

func NewInMemoryOutbox() *InMemoryOutbox {
	return &InMemoryOutbox{
		records: make(map[string]*events.Record),
		claimed: make(map[string]time.Time),
	}
}

func (o *InMemoryOutbox) Append(ctx context.Context, tx events.Tx, e events.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	record := &events.Record{
		ID:        e.ID,
		Event:     e,
		Status:    events.StatusPending,
		Attempts:  0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	o.records[e.ID] = record
	return nil
}

func (o *InMemoryOutbox) Claim(ctx context.Context, limit int) ([]events.Record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var result []events.Record
	now := time.Now().UTC()
	count := 0

	for id, record := range o.records {
		if count >= limit {
			break
		}

		if record.Status != events.StatusPending {
			continue
		}

		if claimTime, claimed := o.claimed[id]; claimed && now.Before(claimTime) {
			continue
		}

		if record.Attempts > 0 && now.Before(record.NextAttempt) {
			continue
		}

		o.claimed[id] = now.Add(30 * time.Second)
		result = append(result, *record)
		count++
	}

	return result, nil
}

func (o *InMemoryOutbox) MarkSent(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	record, exists := o.records[id]
	if !exists {
		return ErrEventNotFound
	}

	record.Status = events.StatusSent
	record.UpdatedAt = time.Now().UTC()
	delete(o.claimed, id)

	return nil
}

func (o *InMemoryOutbox) MarkFailed(ctx context.Context, id string, err error, nextAttempt time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	record, exists := o.records[id]
	if !exists {
		return ErrEventNotFound
	}

	record.Attempts++
	record.LastError = err.Error()
	record.NextAttempt = nextAttempt.UTC()
	record.UpdatedAt = time.Now().UTC()

	if nextAttempt.IsZero() {
		record.Status = events.StatusDeadLetter
	} else if record.Attempts >= 8 {
		record.Status = events.StatusDeadLetter
	} else {
		record.Status = events.StatusPending
	}

	delete(o.claimed, id)

	return nil
}

func (o *InMemoryOutbox) Get(id string) (*events.Record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	record, exists := o.records[id]
	if !exists {
		return nil, ErrEventNotFound
	}

	recordCopy := *record
	return &recordCopy, nil
}

func (o *InMemoryOutbox) Pending() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	count := 0
	for _, record := range o.records {
		if record.Status == events.StatusPending {
			count++
		}
	}
	return count
}

type PostgresOutbox struct {
	db *sql.DB
}

func NewPostgresOutbox(db *sql.DB) *PostgresOutbox {
	return &PostgresOutbox{db: db}
}

func (o *PostgresOutbox) Append(ctx context.Context, tx events.Tx, e events.Event) error {
	dbTx, ok := tx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("expected *sql.Tx, got %T", tx)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	query := `
		INSERT INTO outbox (id, event_type, aggregate_id, event_data, status, attempts, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err = dbTx.ExecContext(ctx, query,
		e.ID,
		e.Type,
		e.AggregateID,
		data,
		events.StatusPending,
		0,
		time.Now().UTC(),
		time.Now().UTC(),
	)

	return err
}

func (o *PostgresOutbox) Claim(ctx context.Context, limit int) ([]events.Record, error) {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	query := `
		UPDATE outbox
		SET status = 'processing', updated_at = $1
		WHERE id IN (
			SELECT id FROM outbox
			WHERE status = 'pending'
			  AND (next_attempt IS NULL OR next_attempt <= $2)
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, event_type, aggregate_id, event_data, status, attempts, next_attempt, last_error, created_at, updated_at
	`

	rows, err := tx.QueryContext(ctx, query, time.Now().UTC(), time.Now().UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var records []events.Record
	for rows.Next() {
		var r events.Record
		var eventData []byte
		var nextAttempt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&r.ID,
			&r.Event.Type,
			&r.Event.AggregateID,
			&eventData,
			&r.Status,
			&r.Attempts,
			&nextAttempt,
			&lastError,
			&r.CreatedAt,
			&r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		if err := json.Unmarshal(eventData, &r.Event); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}

		if nextAttempt.Valid {
			r.NextAttempt = nextAttempt.Time
		}
		if lastError.Valid {
			r.LastError = lastError.String
		}

		records = append(records, r)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return records, nil
}

func (o *PostgresOutbox) MarkSent(ctx context.Context, id string) error {
	query := `
		UPDATE outbox
		SET status = $1, updated_at = $2
		WHERE id = $3
	`

	_, err := o.db.ExecContext(ctx, query, events.StatusSent, time.Now().UTC(), id)
	return err
}

func (o *PostgresOutbox) MarkFailed(ctx context.Context, id string, err error, nextAttempt time.Time) error {
	query := `
		UPDATE outbox
		SET status = $1, last_error = $2, next_attempt = $3, attempts = attempts + 1, updated_at = $4
		WHERE id = $5
	`

	status := events.StatusPending
	attempts := 0

	err2 := o.db.QueryRowContext(ctx, "SELECT attempts FROM outbox WHERE id = $1", id).Scan(&attempts)
	if err2 != nil && err2 != sql.ErrNoRows {
		return err2
	}

	if nextAttempt.IsZero() {
		status = events.StatusDeadLetter
	} else if attempts+1 >= 8 {
		status = events.StatusDeadLetter
	}

	_, err = o.db.ExecContext(ctx, query, status, err.Error(), nextAttempt.UTC(), time.Now().UTC(), id)
	return err
}

func NextBackoff(attempt int) time.Time {
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
