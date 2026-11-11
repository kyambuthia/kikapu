package idempotency

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

type PostgresStore struct {
	db      *sql.DB
	ttl     time.Duration
	timeout time.Duration
}

func NewPostgresStore(db *sql.DB, ttl time.Duration) *PostgresStore {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	return &PostgresStore{
		db:      db,
		ttl:     ttl,
		timeout: 30 * time.Second,
	}
}

func (s *PostgresStore) Begin(ctx context.Context, key, requestHash string) (Record, bool, error) {
	var rec Record
	var status string
	var responseStatus sql.NullInt32
	var responseBody []byte
	var createdAt, expiresAt time.Time

	query := `
		SELECT key, request_hash, status, response_status, response_body, created_at, expires_at
		FROM idempotency_keys
		WHERE key = $1`

	err := s.db.QueryRowContext(ctx, query, key).Scan(
		&rec.Key, &rec.RequestHash, &status, &responseStatus, &responseBody,
		&createdAt, &expiresAt,
	)

	if err == sql.ErrNoRows {
		insertQuery := `
			INSERT INTO idempotency_keys (key, request_hash, status, expires_at)
			VALUES ($1, $2, 'pending', $3)
			ON CONFLICT (key) DO NOTHING`

		result, err := s.db.ExecContext(ctx, insertQuery, key, requestHash, time.Now().Add(s.ttl))
		if err != nil {
			return Record{}, false, err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return Record{}, false, err
		}

		if rows == 0 {
			return s.Begin(ctx, key, requestHash)
		}

		return Record{}, false, nil
	}

	if err != nil {
		return Record{}, false, err
	}

	if expiresAt.Before(time.Now()) {
		deleteQuery := `DELETE FROM idempotency_keys WHERE key = $1`
		s.db.ExecContext(ctx, deleteQuery, key)
		return Record{}, false, ErrExpired
	}

	if rec.RequestHash != requestHash {
		return Record{}, false, ErrConflict
	}

	if status == "pending" {
		if time.Since(createdAt) > s.timeout {
			return Record{}, false, ErrInFlight
		}
		return Record{}, false, ErrInFlight
	}

	rec.Status = int(responseStatus.Int32)
	rec.Response = json.RawMessage(responseBody)
	rec.CreatedAt = createdAt
	rec.ExpiresAt = expiresAt

	return rec, true, nil
}

func (s *PostgresStore) Finish(ctx context.Context, key string, status int, body []byte) error {
	query := `
		UPDATE idempotency_keys
		SET status = 'done', response_status = $1, response_body = $2
		WHERE key = $3`

	_, err := s.db.ExecContext(ctx, query, status, body, key)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return nil
		}
		return err
	}

	return nil
}
