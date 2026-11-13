package order

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, id string) (*Order, error) {
	query := `
		SELECT id, number, region_id, status, payment_status, fulfillment_status,
		       subtotal, discount, tax, grand_total, currency, created_at, updated_at
		FROM orders
		WHERE id = $1
	`

	var o Order
	var currency string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&o.ID, &o.Number, &o.RegionID, &o.Status, &o.PaymentStatus, &o.FulfillmentStatus,
		&o.Totals.Subtotal.Value, &o.Totals.Discount.Value, &o.Totals.Tax.Value, &o.Totals.GrandTotal.Value,
		&currency, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("order: get order %s: %w", id, err)
	}

	o.Totals.Subtotal.Currency = currency
	o.Totals.Discount.Currency = currency
	o.Totals.Tax.Currency = currency
	o.Totals.GrandTotal.Currency = currency

	lines, err := s.getLines(ctx, o.ID)
	if err != nil {
		return nil, fmt.Errorf("order: get lines: %w", err)
	}
	o.Lines = lines

	refunds, err := s.getRefunds(ctx, o.ID)
	if err != nil {
		return nil, fmt.Errorf("order: get refunds: %w", err)
	}
	o.Refunds = refunds

	return &o, nil
}

func (s *PostgresStore) Create(ctx context.Context, tx *sql.Tx, o *Order) error {
	query := `
		INSERT INTO orders (id, number, region_id, status, payment_status, fulfillment_status,
		                    subtotal, discount, tax, grand_total, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	var execer interface {
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
	if tx != nil {
		execer = tx
	} else {
		execer = s.db
	}

	_, err := execer.ExecContext(ctx, query,
		o.ID, o.Number, o.RegionID, o.Status, o.PaymentStatus, o.FulfillmentStatus,
		o.Totals.Subtotal.Value, o.Totals.Discount.Value, o.Totals.Tax.Value, o.Totals.GrandTotal.Value,
		o.Totals.Subtotal.Currency, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("order: create order: %w", err)
	}

	for _, line := range o.Lines {
		if err := s.createLine(ctx, execer, o.ID, line); err != nil {
			return fmt.Errorf("order: create line: %w", err)
		}
	}

	return nil
}

func (s *PostgresStore) Save(ctx context.Context, o *Order) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("order: begin tx: %w", err)
	}
	defer tx.Rollback()

	updateQuery := `
		UPDATE orders
		SET status = $1, payment_status = $2, fulfillment_status = $3,
		    subtotal = $4, discount = $5, tax = $6, grand_total = $7,
		    updated_at = $8
		WHERE id = $9
	`

	_, err = tx.ExecContext(ctx, updateQuery,
		o.Status, o.PaymentStatus, o.FulfillmentStatus,
		o.Totals.Subtotal.Value, o.Totals.Discount.Value, o.Totals.Tax.Value, o.Totals.GrandTotal.Value,
		o.UpdatedAt, o.ID,
	)
	if err != nil {
		return fmt.Errorf("order: update order: %w", err)
	}

	for _, refund := range o.Refunds {
		if err := s.createRefund(ctx, tx, o.ID, refund); err != nil {
			return fmt.Errorf("order: create refund: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("order: commit tx: %w", err)
	}

	return nil
}

func (s *PostgresStore) getLines(ctx context.Context, orderID string) ([]Line, error) {
	query := `
		SELECT variant_id, sku, quantity, unit_price, discount, tax, currency
		FROM order_lines
		WHERE order_id = $1
		ORDER BY created_at
	`

	rows, err := s.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("query lines: %w", err)
	}
	defer rows.Close()

	var lines []Line

	for rows.Next() {
		var l Line
		var currency string
		if err := rows.Scan(
			&l.VariantID, &l.SKU, &l.Quantity,
			&l.UnitPrice.Value, &l.Discount.Value, &l.Tax.Value, &currency,
		); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		l.UnitPrice.Currency = currency
		l.Discount.Currency = currency
		l.Tax.Currency = currency
		lines = append(lines, l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lines: %w", err)
	}

	return lines, nil
}

func (s *PostgresStore) getRefunds(ctx context.Context, orderID string) ([]Refund, error) {
	query := `
		SELECT id, amount, currency, reason, created_at
		FROM refunds
		WHERE order_id = $1
		ORDER BY created_at
	`

	rows, err := s.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("query refunds: %w", err)
	}
	defer rows.Close()

	var refunds []Refund

	for rows.Next() {
		var r Refund
		var currency string
		if err := rows.Scan(&r.ID, &r.Amount.Value, &currency, &r.Reason, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan refund: %w", err)
		}
		r.Amount.Currency = currency
		refunds = append(refunds, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refunds: %w", err)
	}

	return refunds, nil
}

func (s *PostgresStore) createLine(ctx context.Context, execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}, orderID string, line Line) error {
	query := `
		INSERT INTO order_lines (order_id, variant_id, sku, quantity, unit_price, discount, tax, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := execer.ExecContext(ctx, query,
		orderID, line.VariantID, line.SKU, line.Quantity,
		line.UnitPrice.Value, line.Discount.Value, line.Tax.Value, line.UnitPrice.Currency, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("insert line: %w", err)
	}

	return nil
}

func (s *PostgresStore) createRefund(ctx context.Context, tx *sql.Tx, orderID string, refund Refund) error {
	query := `
		INSERT INTO refunds (id, order_id, amount, currency, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`

	_, err := tx.ExecContext(ctx, query,
		refund.ID, orderID, refund.Amount.Value, refund.Amount.Currency, refund.Reason, refund.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert refund: %w", err)
	}

	return nil
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, id string, status Status, paymentStatus PaymentStatus, updatedAt time.Time) error {
	query := `
		UPDATE orders
		SET status = $1, payment_status = $2, updated_at = $3
		WHERE id = $4
	`

	_, err := s.db.ExecContext(ctx, query, status, paymentStatus, updatedAt, id)
	if err != nil {
		return fmt.Errorf("order: update status: %w", err)
	}

	return nil
}
