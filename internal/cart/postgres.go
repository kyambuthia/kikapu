package cart

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PostgresStore persists carts in PostgreSQL with optimistic version locking.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(id string) (*Cart, error) {
	ctx := context.Background()
	var (
		c         Cart
		linesJSON []byte
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, currency, lines, version, updated_at
		FROM carts
		WHERE id = $1
	`, id).Scan(&c.ID, &c.Currency, &linesJSON, &c.Version, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCartNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cart: get %s: %w", id, err)
	}

	if err := json.Unmarshal(linesJSON, &c.Lines); err != nil {
		return nil, fmt.Errorf("cart: unmarshal lines: %w", err)
	}
	if c.Lines == nil {
		c.Lines = []Line{}
	}
	return &c, nil
}

func (s *PostgresStore) Save(c *Cart) error {
	ctx := context.Background()
	linesJSON, err := json.Marshal(c.Lines)
	if err != nil {
		return fmt.Errorf("cart: marshal lines: %w", err)
	}

	if c.Version <= 1 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO carts (id, currency, lines, version, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING
		`, c.ID, c.Currency, linesJSON, c.Version, c.UpdatedAt)
		if err != nil {
			return fmt.Errorf("cart: insert %s: %w", c.ID, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("cart: rows affected: %w", err)
		}
		if rows == 0 {
			return ErrVersionMismatch
		}
		return nil
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE carts
		SET currency = $2, lines = $3, version = $4, updated_at = $5
		WHERE id = $1 AND version = $6
	`, c.ID, c.Currency, linesJSON, c.Version, c.UpdatedAt, c.Version-1)
	if err != nil {
		return fmt.Errorf("cart: update %s: %w", c.ID, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("cart: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrVersionMismatch
	}
	return nil
}
