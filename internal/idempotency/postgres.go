package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type PostgresStore struct {
	db             *sql.DB
	requestTimeout time.Duration
	ttl            time.Duration
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{
		db:             db,
		requestTimeout: 30 * time.Second,
		ttl:            24 * time.Hour,
	}
}

func (s *PostgresStore) Begin(ctx context.Context, key, requestHash string) (Record, bool, error) {
	keyHash := sha256.Sum256([]byte(key))
	reqHash := sha256.Sum256([]byte(requestHash))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, false, fmt.Errorf("idempotency: begin tx: %w", err)
	}
	defer tx.Rollback()

	var storedKeyHash, storedReqHash []byte
	var status string
	var responseStatus sql.NullInt32
	var responseBody []byte
	var createdAt, expiresAt time.Time

	query := `
		SELECT key_hash, request_hash, status, response_status, response_body, created_at, expires_at
		FROM idempotency_records
		WHERE key_hash = $1 AND expires_at > now()
	`

	err = tx.QueryRowContext(ctx, query, keyHash[:]).Scan(
		&storedKeyHash, &storedReqHash, &status, &responseStatus, &responseBody, &createdAt, &expiresAt,
	)

	if err == nil {
		if string(reqHash[:]) != string(storedReqHash) {
			return Record{}, false, ErrConflict
		}

		if status == string(StatusPending) {
			if time.Since(createdAt) > s.requestTimeout {
				return Record{}, false, ErrInFlight
			}
			return Record{}, false, ErrInFlight
		}

		rec := Record{
			Key:         key,
			RequestHash: requestHash,
			Status:      int(responseStatus.Int32),
			Response:    json.RawMessage(responseBody),
			CreatedAt:   createdAt,
			ExpiresAt:   expiresAt,
		}
		return rec, true, nil
	}

	if err != sql.ErrNoRows {
		return Record{}, false, fmt.Errorf("idempotency: query: %w", err)
	}

	now := time.Now()
	insertQuery := `
		INSERT INTO idempotency_records (key_hash, request_hash, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err = tx.ExecContext(ctx, insertQuery, keyHash[:], reqHash[:], StatusPending, now, now.Add(s.ttl))
	if err != nil {
		if isUniqueViolation(err) {
			var retryStatus string
			var retryReqHash []byte
			var retryRespStatus sql.NullInt32
			var retryRespBody []byte
			var retryCreated, retryExpires time.Time

			retryQuery := `
				SELECT request_hash, status, response_status, response_body, created_at, expires_at
				FROM idempotency_records
				WHERE key_hash = $1
			`
			err2 := tx.QueryRowContext(ctx, retryQuery, keyHash[:]).Scan(
				&retryReqHash, &retryStatus, &retryRespStatus, &retryRespBody, &retryCreated, &retryExpires,
			)
			if err2 != nil {
				return Record{}, false, fmt.Errorf("idempotency: retry query: %w", err2)
			}

			if string(reqHash[:]) != string(retryReqHash) {
				return Record{}, false, ErrConflict
			}

			if retryStatus == string(StatusPending) {
				return Record{}, false, ErrInFlight
			}

			rec := Record{
				Key:         key,
				RequestHash: requestHash,
				Status:      int(retryRespStatus.Int32),
				Response:    json.RawMessage(retryRespBody),
				CreatedAt:   retryCreated,
				ExpiresAt:   retryExpires,
			}
			return rec, true, nil
		}
		return Record{}, false, fmt.Errorf("idempotency: insert pending: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Record{}, false, fmt.Errorf("idempotency: commit tx: %w", err)
	}

	return Record{
		Key:         key,
		RequestHash: requestHash,
		Status:      0,
		Response:    nil,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.ttl),
	}, false, nil
}

func (s *PostgresStore) Finish(ctx context.Context, key string, status int, body []byte) error {
	keyHash := sha256.Sum256([]byte(key))

	query := `
		UPDATE idempotency_records
		SET status = $1, response_status = $2, response_body = $3
		WHERE key_hash = $4
	`

	result, err := s.db.ExecContext(ctx, query, StatusDone, status, body, keyHash[:])
	if err != nil {
		return fmt.Errorf("idempotency: finish: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("idempotency: rows affected: %w", err)
	}

	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *PostgresStore) Cleanup(ctx context.Context) error {
	query := `DELETE FROM idempotency_records WHERE expires_at < now()`

	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("idempotency: cleanup: %w", err)
	}

	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "pq: duplicate key value violates unique constraint \"idempotency_records_pkey\"" ||
		err.Error() == "ERROR: duplicate key value violates unique constraint \"idempotency_records_pkey\" (SQLSTATE 23505)"
}
