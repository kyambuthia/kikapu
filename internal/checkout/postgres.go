package checkout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PostgresOrderStore implements OrderStore backed by PostgreSQL.
type PostgresOrderStore struct {
	db *sql.DB
}

// NewPostgresOrderStore creates a PostgreSQL-backed checkout OrderStore.
func NewPostgresOrderStore(db *sql.DB) *PostgresOrderStore {
	return &PostgresOrderStore{db: db}
}

func (s *PostgresOrderStore) GetOrderForKey(key string) (*Order, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	o, err := s.scanOrder(ctx, `
		SELECT id, cart_id, status, total, currency, payment_id, reservations, created_at, updated_at
		FROM checkout_orders
		WHERE idempotency_key = $1
	`, key)
	if err != nil {
		return nil, false
	}
	return o, true
}

func (s *PostgresOrderStore) SaveOrder(key string, order *Order) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resJSON, err := json.Marshal(order.Reservations)
	if err != nil {
		return fmt.Errorf("checkout: marshal reservations: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO checkout_orders (id, idempotency_key, cart_id, status, total, currency, payment_id, reservations, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, order.ID, key, order.CartID, string(order.Status), order.Total, order.Currency, order.PaymentID, resJSON, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return fmt.Errorf("checkout: save order: %w", err)
	}

	if existing, ok := s.GetOrderForKey(key); ok {
		*order = *existing
	}
	return nil
}

func (s *PostgresOrderStore) GetOrder(id string) (*Order, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	o, err := s.scanOrder(ctx, `
		SELECT id, cart_id, status, total, currency, payment_id, reservations, created_at, updated_at
		FROM checkout_orders
		WHERE id = $1
	`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkout: get order: %w", err)
	}
	return o, nil
}

func (s *PostgresOrderStore) scanOrder(ctx context.Context, query string, arg any) (*Order, error) {
	var o Order
	var status string
	var resJSON []byte
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&o.ID, &o.CartID, &status, &o.Total, &o.Currency, &o.PaymentID, &resJSON, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	o.Status = OrderStatus(status)
	if len(resJSON) > 0 {
		_ = json.Unmarshal(resJSON, &o.Reservations)
	}
	if o.Reservations == nil {
		o.Reservations = []string{}
	}
	return &o, nil
}
