package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// PostgresSubscriberStore implements SubscriberStore backed by PostgreSQL.
type PostgresSubscriberStore struct {
	db *sql.DB
}

// NewPostgresSubscriberStore creates a PostgreSQL-backed SubscriberStore.
func NewPostgresSubscriberStore(db *sql.DB) *PostgresSubscriberStore {
	return &PostgresSubscriberStore{db: db}
}

// Add inserts or updates a webhook subscription.
func (s *PostgresSubscriberStore) Add(ctx context.Context, sub Subscription) error {
	typesJSON, err := json.Marshal(sub.Types)
	if err != nil {
		return fmt.Errorf("worker: marshal types: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO webhook_subscriptions (id, url, secret, types, active)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET url = EXCLUDED.url, secret = EXCLUDED.secret, types = EXCLUDED.types, active = EXCLUDED.active
	`, sub.ID, sub.URL, sub.Secret, typesJSON, sub.Active)
	if err != nil {
		return fmt.Errorf("worker: add subscription: %w", err)
	}
	return nil
}

func (s *PostgresSubscriberStore) List(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, secret, types, active
		FROM webhook_subscriptions
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("worker: list subscriptions: %w", err)
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func (s *PostgresSubscriberStore) ListByEventType(ctx context.Context, eventType string) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, secret, types, active
		FROM webhook_subscriptions
		WHERE active = true AND (types ? $1 OR types ? '*')
		ORDER BY id
	`, eventType)
	if err != nil {
		return nil, fmt.Errorf("worker: list subscriptions by event: %w", err)
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func scanSubscriptions(rows *sql.Rows) ([]Subscription, error) {
	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		var typesJSON []byte
		if err := rows.Scan(&sub.ID, &sub.URL, &sub.Secret, &typesJSON, &sub.Active); err != nil {
			return nil, fmt.Errorf("worker: scan subscription: %w", err)
		}
		if len(typesJSON) > 0 {
			_ = json.Unmarshal(typesJSON, &sub.Types)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}
