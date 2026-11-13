package inventory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// PostgresStore implements inventory.Store backed by PostgreSQL.
type PostgresStore struct {
	db    *sql.DB
	clock Clock
}

// NewPostgresStore creates a PostgreSQL-backed inventory Store.
func NewPostgresStore(db *sql.DB, clock Clock) *PostgresStore {
	if clock == nil {
		clock = RealClock{}
	}
	return &PostgresStore{db: db, clock: clock}
}

// SetLevel upserts the on-hand stock level for a variant at a location.
func (s *PostgresStore) SetLevel(ctx context.Context, variantID, locationID string, onHand int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stock_levels (variant_id, location_id, on_hand, reserved, created_at, updated_at)
		VALUES ($1, $2, $3, 0, now(), now())
		ON CONFLICT (variant_id, location_id) DO UPDATE
		SET on_hand = EXCLUDED.on_hand, updated_at = now()
	`, variantID, locationID, onHand)
	if err != nil {
		return fmt.Errorf("inventory: set level: %w", err)
	}
	return nil
}

// Levels returns all stock levels for the given variant.
func (s *PostgresStore) Levels(ctx context.Context, variantID string) ([]Level, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT variant_id, location_id, on_hand, reserved
		FROM stock_levels
		WHERE variant_id = $1
		ORDER BY location_id
	`, variantID)
	if err != nil {
		return nil, fmt.Errorf("inventory: list levels: %w", err)
	}
	defer rows.Close()

	var levels []Level
	for rows.Next() {
		var l Level
		if err := rows.Scan(&l.VariantID, &l.LocationID, &l.OnHand, &l.Reserved); err != nil {
			return nil, fmt.Errorf("inventory: scan level: %w", err)
		}
		levels = append(levels, l)
	}
	return levels, rows.Err()
}

// Reserve atomically reserves qty units of variantID from the tightest-fitting location.
func (s *PostgresStore) Reserve(ctx context.Context, variantID string, qty int, ttl time.Duration) (Reservation, error) {
	if qty <= 0 {
		return Reservation{}, ErrInsufficientStock
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, fmt.Errorf("inventory: begin tx: %w", err)
	}
	defer tx.Rollback()

	var locationID string
	err = tx.QueryRowContext(ctx, `
		SELECT location_id
		FROM stock_levels
		WHERE variant_id = $1 AND (on_hand - reserved) >= $2
		ORDER BY (on_hand - reserved) ASC, location_id ASC
		LIMIT 1
		FOR UPDATE
	`, variantID, qty).Scan(&locationID)
	if errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, ErrInsufficientStock
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("inventory: lock stock level: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE stock_levels
		SET reserved = reserved + $3, updated_at = now()
		WHERE variant_id = $1 AND location_id = $2
	`, variantID, locationID, qty); err != nil {
		return Reservation{}, fmt.Errorf("inventory: increment reserved: %w", err)
	}

	now := s.clock.Now().UTC()
	res := Reservation{
		ID:         newReservationID(),
		VariantID:  variantID,
		LocationID: locationID,
		Qty:        qty,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_reservations (id, variant_id, location_id, qty, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, res.ID, res.VariantID, res.LocationID, res.Qty, res.ExpiresAt, res.CreatedAt); err != nil {
		return Reservation{}, fmt.Errorf("inventory: insert reservation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Reservation{}, fmt.Errorf("inventory: commit reserve: %w", err)
	}
	return res, nil
}

// Release cancels an active reservation and returns reserved stock.
func (s *PostgresStore) Release(ctx context.Context, reservationID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("inventory: begin tx: %w", err)
	}
	defer tx.Rollback()

	var variantID, locationID string
	var qty int
	err = tx.QueryRowContext(ctx, `
		DELETE FROM inventory_reservations
		WHERE id = $1
		RETURNING variant_id, location_id, qty
	`, reservationID).Scan(&variantID, &locationID, &qty)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReservationNotFound
	}
	if err != nil {
		return fmt.Errorf("inventory: delete reservation: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE stock_levels
		SET reserved = GREATEST(0, reserved - $3), updated_at = now()
		WHERE variant_id = $1 AND location_id = $2
	`, variantID, locationID, qty); err != nil {
		return fmt.Errorf("inventory: release reserved: %w", err)
	}

	return tx.Commit()
}

// Commit finalizes a reservation by deducting both on_hand and reserved stock.
func (s *PostgresStore) Commit(ctx context.Context, reservationID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("inventory: begin tx: %w", err)
	}
	defer tx.Rollback()

	var variantID, locationID string
	var qty int
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
		DELETE FROM inventory_reservations
		WHERE id = $1
		RETURNING variant_id, location_id, qty, expires_at
	`, reservationID).Scan(&variantID, &locationID, &qty, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReservationNotFound
	}
	if err != nil {
		return fmt.Errorf("inventory: delete reservation: %w", err)
	}

	if expiresAt.Before(s.clock.Now()) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE stock_levels
			SET reserved = GREATEST(0, reserved - $3), updated_at = now()
			WHERE variant_id = $1 AND location_id = $2
		`, variantID, locationID, qty); err != nil {
			return fmt.Errorf("inventory: release expired reserved: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("inventory: commit expired release: %w", err)
		}
		return ErrReservationExpired
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE stock_levels
		SET on_hand = on_hand - $3, reserved = reserved - $3, updated_at = now()
		WHERE variant_id = $1 AND location_id = $2
	`, variantID, locationID, qty); err != nil {
		return fmt.Errorf("inventory: commit stock: %w", err)
	}

	return tx.Commit()
}

// ExpireDue releases all reservations whose expires_at is before now.
func (s *PostgresStore) ExpireDue(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("inventory: begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		DELETE FROM inventory_reservations
		WHERE expires_at < $1
		RETURNING variant_id, location_id, qty
	`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("inventory: delete expired reservations: %w", err)
	}

	type releaseItem struct {
		variantID  string
		locationID string
		qty        int
	}
	var items []releaseItem
	for rows.Next() {
		var it releaseItem
		if err := rows.Scan(&it.variantID, &it.locationID, &it.qty); err != nil {
			rows.Close()
			return 0, fmt.Errorf("inventory: scan expired reservation: %w", err)
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `
			UPDATE stock_levels
			SET reserved = GREATEST(0, reserved - $3), updated_at = now()
			WHERE variant_id = $1 AND location_id = $2
		`, it.variantID, it.locationID, it.qty); err != nil {
			return 0, fmt.Errorf("inventory: release expired stock: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("inventory: commit expire due: %w", err)
	}
	return len(items), nil
}

func newReservationID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "res_" + hex.EncodeToString(b[:])
}
