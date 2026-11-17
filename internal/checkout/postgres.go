package checkout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/payment"
)

// PostgresOrderStore implements OrderStore and TransactionalOrderStore backed by PostgreSQL.
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

	return s.SaveOrderWithEvents(ctx, key, order, nil)
}

// SaveOrderWithEvents atomically inserts the order and appends its outbox events
// within a single database transaction. If the idempotency key already exists,
// no duplicate outbox events are emitted and order is populated with the stored row.
func (s *PostgresOrderStore) SaveOrderWithEvents(ctx context.Context, key string, order *Order, ob events.Outbox, evs ...events.Event) error {
	return s.SaveOrderWithEventsAndLines(ctx, key, order, nil, ob, evs...)
}

func (s *PostgresOrderStore) SaveOrderWithEventsAndLines(ctx context.Context, key string, order *Order, lines []cart.Line, ob events.Outbox, evs ...events.Event) error {
	resJSON, err := json.Marshal(order.Reservations)
	if err != nil {
		return fmt.Errorf("checkout: marshal reservations: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkout: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO checkout_orders (id, idempotency_key, cart_id, status, total, currency, payment_id, reservations, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, order.ID, key, order.CartID, string(order.Status), order.Total, order.Currency, order.PaymentID, resJSON, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return fmt.Errorf("checkout: save order: %w", err)
	}

	inserted, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checkout: rows affected: %w", err)
	}

	if inserted > 0 {
		if _, err := uuid.Parse(order.ID); err == nil {
			ordStatus := "confirmed"
			payStatus := "captured"
			if order.Status == OrderStatusFailed {
				ordStatus = "cancelled"
				payStatus = "pending"
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO orders (id, number, region_id, status, payment_status, fulfillment_status, subtotal, discount, tax, grand_total, currency, created_at, updated_at)
				VALUES ($1, $2, 'default', $3, $4, 'unfulfilled', $5, 0, 0, $5, $6, $7, $8)
				ON CONFLICT (id) DO NOTHING
			`, order.ID, order.ID, ordStatus, payStatus, order.Total, order.Currency, order.CreatedAt, order.UpdatedAt); err != nil {
				return fmt.Errorf("checkout: insert canonical order: %w", err)
			}
			for _, line := range lines {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO order_lines (order_id, variant_id, sku, quantity, unit_price, discount, tax, currency, created_at)
					VALUES ($1, $2, $3, $4, $5, 0, 0, $6, $7)
				`, order.ID, line.VariantID, line.VariantID, line.Quantity, line.UnitPrice.Value, line.UnitPrice.Currency, order.CreatedAt); err != nil {
					return fmt.Errorf("checkout: insert order line: %w", err)
				}
			}
		}

		if ob != nil {
			for i := range evs {
				if err := ob.Append(ctx, tx, evs[i]); err != nil {
					return fmt.Errorf("checkout: append outbox event: %w", err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkout: commit tx: %w", err)
	}

	if inserted == 0 {
		if existing, ok := s.GetOrderForKey(key); ok {
			*order = *existing
		}
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

// ReconcilePayment updates the checkout order status matching paymentID when an
// asynchronous payment webhook or callback reports a terminal payment state.
func (s *PostgresOrderStore) ReconcilePayment(ctx context.Context, paymentID string, state payment.State) error {
	if paymentID == "" {
		return nil
	}
	targetStatus := mapPaymentStateToOrderStatus(state)
	if targetStatus == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE checkout_orders
		SET status = $1, updated_at = $2
		WHERE payment_id = $3 AND status <> $4
	`, string(targetStatus), time.Now().UTC(), paymentID, string(OrderStatusPaymentCapturedFulfilmentFailed))
	return err
}
