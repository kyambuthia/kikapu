package fulfillment

import (
	"context"
	"database/sql"
	"time"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(f *Fulfillment) error {
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO fulfillments (id, order_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, f.ID, f.OrderID, f.Status, f.CreatedAt, f.UpdatedAt)
	if err != nil {
		return err
	}

	for _, item := range f.Items {
		_, err = tx.Exec(`
			INSERT INTO fulfillment_items (fulfillment_id, variant_id, quantity)
			VALUES ($1, $2, $3)
		`, f.ID, item.VariantID, item.Quantity)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *PostgresRepository) Get(id string) (*Fulfillment, error) {
	var f Fulfillment
	var carrier, trackingNumber, trackingURL sql.NullString

	err := r.db.QueryRow(`
		SELECT id, order_id, status, carrier, tracking_number, tracking_url, created_at, updated_at
		FROM fulfillments
		WHERE id = $1
	`, id).Scan(&f.ID, &f.OrderID, &f.Status, &carrier, &trackingNumber, &trackingURL, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, err
	}

	if carrier.Valid && trackingNumber.Valid {
		f.Tracking = &Tracking{
			Carrier: carrier.String,
			Number:  trackingNumber.String,
			URL:     trackingURL.String,
		}
	}

	rows, err := r.db.Query(`
		SELECT variant_id, quantity
		FROM fulfillment_items
		WHERE fulfillment_id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.VariantID, &item.Quantity); err != nil {
			return nil, err
		}
		f.Items = append(f.Items, item)
	}

	return &f, rows.Err()
}

func (r *PostgresRepository) ListByOrder(orderID string) ([]*Fulfillment, error) {
	rows, err := r.db.Query(`
		SELECT id, order_id, status, carrier, tracking_number, tracking_url, created_at, updated_at
		FROM fulfillments
		WHERE order_id = $1
		ORDER BY created_at
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fulfillments []*Fulfillment
	for rows.Next() {
		var f Fulfillment
		var carrier, trackingNumber, trackingURL sql.NullString

		if err := rows.Scan(&f.ID, &f.OrderID, &f.Status, &carrier, &trackingNumber, &trackingURL, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}

		if carrier.Valid && trackingNumber.Valid {
			f.Tracking = &Tracking{
				Carrier: carrier.String,
				Number:  trackingNumber.String,
				URL:     trackingURL.String,
			}
		}

		fulfillments = append(fulfillments, &f)
	}

	for _, f := range fulfillments {
		itemRows, err := r.db.Query(`
			SELECT variant_id, quantity
			FROM fulfillment_items
			WHERE fulfillment_id = $1
		`, f.ID)
		if err != nil {
			return nil, err
		}

		for itemRows.Next() {
			var item Item
			if err := itemRows.Scan(&item.VariantID, &item.Quantity); err != nil {
				itemRows.Close()
				return nil, err
			}
			f.Items = append(f.Items, item)
		}
		itemRows.Close()
	}

	return fulfillments, rows.Err()
}

func (r *PostgresRepository) Update(f *Fulfillment) error {
	_, err := r.db.Exec(`
		UPDATE fulfillments
		SET status = $1, updated_at = $2
		WHERE id = $3
	`, f.Status, f.UpdatedAt, f.ID)
	return err
}

func (r *PostgresRepository) AddTracking(id string, t Tracking) error {
	_, err := r.db.Exec(`
		UPDATE fulfillments
		SET carrier = $1, tracking_number = $2, tracking_url = $3, updated_at = $4
		WHERE id = $5
	`, t.Carrier, t.Number, t.URL, time.Now(), id)
	return err
}
