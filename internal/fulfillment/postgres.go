package fulfillment

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(ctx context.Context, f *Fulfillment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fulfillment: begin tx: %w", err)
	}
	defer tx.Rollback()

	if f.ID == "" {
		f.ID = uuid.New().String()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO fulfillments (id, order_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, f.ID, f.OrderID, f.Status, f.CreatedAt, f.UpdatedAt)
	if err != nil {
		return fmt.Errorf("fulfillment: insert fulfillment: %w", err)
	}

	for _, item := range f.Items {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO fulfillment_items (fulfillment_id, variant_id, quantity, created_at)
			VALUES ($1, $2, $3, $4)
		`, f.ID, item.VariantID, item.Quantity, f.CreatedAt)
		if err != nil {
			return fmt.Errorf("fulfillment: insert item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fulfillment: commit tx: %w", err)
	}

	return nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (*Fulfillment, error) {
	query := `
		SELECT id, order_id, status, created_at, updated_at
		FROM fulfillments
		WHERE id = $1
	`

	var f Fulfillment
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&f.ID, &f.OrderID, &f.Status, &f.CreatedAt, &f.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fulfillment: get fulfillment: %w", err)
	}

	items, err := r.getItems(ctx, f.ID)
	if err != nil {
		return nil, fmt.Errorf("fulfillment: get items: %w", err)
	}
	f.Items = items

	tracking, err := r.getTracking(ctx, f.ID)
	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("fulfillment: get tracking: %w", err)
	}
	if tracking != nil {
		f.Tracking = tracking
	}

	return &f, nil
}

func (r *PostgresRepository) Update(ctx context.Context, f *Fulfillment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fulfillment: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE fulfillments
		SET status = $1, updated_at = $2
		WHERE id = $3
	`, f.Status, f.UpdatedAt, f.ID)
	if err != nil {
		return fmt.Errorf("fulfillment: update fulfillment: %w", err)
	}

	if f.Tracking != nil {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tracking (fulfillment_id, carrier, number, url, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (fulfillment_id) DO UPDATE SET
				carrier = EXCLUDED.carrier,
				number = EXCLUDED.number,
				url = EXCLUDED.url
		`, f.ID, f.Tracking.Carrier, f.Tracking.Number, f.Tracking.URL, time.Now())
		if err != nil {
			return fmt.Errorf("fulfillment: upsert tracking: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fulfillment: commit tx: %w", err)
	}

	return nil
}

func (r *PostgresRepository) ListByOrder(ctx context.Context, orderID string) ([]Fulfillment, error) {
	query := `
		SELECT id, order_id, status, created_at, updated_at
		FROM fulfillments
		WHERE order_id = $1
		ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("fulfillment: list fulfillments: %w", err)
	}
	defer rows.Close()

	var fulfillments []Fulfillment
	for rows.Next() {
		var f Fulfillment
		if err := rows.Scan(&f.ID, &f.OrderID, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("fulfillment: scan fulfillment: %w", err)
		}
		fulfillments = append(fulfillments, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fulfillment: iterate fulfillments: %w", err)
	}

	for i := range fulfillments {
		items, err := r.getItems(ctx, fulfillments[i].ID)
		if err != nil {
			return nil, fmt.Errorf("fulfillment: get items: %w", err)
		}
		fulfillments[i].Items = items

		tracking, err := r.getTracking(ctx, fulfillments[i].ID)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("fulfillment: get tracking: %w", err)
		}
		if tracking != nil {
			fulfillments[i].Tracking = tracking
		}
	}

	return fulfillments, nil
}

func (r *PostgresRepository) getItems(ctx context.Context, fulfillmentID string) ([]Item, error) {
	query := `
		SELECT variant_id, quantity
		FROM fulfillment_items
		WHERE fulfillment_id = $1
		ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, fulfillmentID)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.VariantID, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}

	return items, nil
}

func (r *PostgresRepository) getTracking(ctx context.Context, fulfillmentID string) (*Tracking, error) {
	query := `
		SELECT carrier, number, url
		FROM tracking
		WHERE fulfillment_id = $1
	`

	var t Tracking
	err := r.db.QueryRowContext(ctx, query, fulfillmentID).Scan(&t.Carrier, &t.Number, &t.URL)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query tracking: %w", err)
	}

	return &t, nil
}
