package region

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PostgresRepository implements region.Repository backed by PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a PostgreSQL-backed region Repository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func toContext(ctx interface{}) context.Context {
	if c, ok := ctx.(context.Context); ok && c != nil {
		return c
	}
	return context.Background()
}

func (r *PostgresRepository) Get(ctx interface{}, id string) (*Region, error) {
	var reg Region
	var countriesJSON []byte
	err := r.db.QueryRowContext(toContext(ctx), `
		SELECT id, name, currency, countries, tax_rate_bps, tax_inclusive
		FROM regions
		WHERE id = $1
	`, id).Scan(&reg.ID, &reg.Name, &reg.Currency, &countriesJSON, &reg.TaxRateBPS, &reg.TaxInclusive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("region: get %s: %w", id, err)
	}
	if len(countriesJSON) > 0 {
		_ = json.Unmarshal(countriesJSON, &reg.Countries)
	}
	return &reg, nil
}

func (r *PostgresRepository) List(ctx interface{}) ([]Region, error) {
	rows, err := r.db.QueryContext(toContext(ctx), `
		SELECT id, name, currency, countries, tax_rate_bps, tax_inclusive
		FROM regions
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("region: list: %w", err)
	}
	defer rows.Close()

	var result []Region
	for rows.Next() {
		var reg Region
		var countriesJSON []byte
		if err := rows.Scan(&reg.ID, &reg.Name, &reg.Currency, &countriesJSON, &reg.TaxRateBPS, &reg.TaxInclusive); err != nil {
			return nil, fmt.Errorf("region: scan: %w", err)
		}
		if len(countriesJSON) > 0 {
			_ = json.Unmarshal(countriesJSON, &reg.Countries)
		}
		result = append(result, reg)
	}
	return result, rows.Err()
}
