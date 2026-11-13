package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PostgresRepository implements channel.Repository backed by PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a PostgreSQL-backed channel Repository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func toContext(ctx interface{}) context.Context {
	if c, ok := ctx.(context.Context); ok && c != nil {
		return c
	}
	return context.Background()
}

func (r *PostgresRepository) Get(ctx interface{}, id string) (*SalesChannel, error) {
	return r.scanOne(toContext(ctx), `SELECT id, name, slug, regions FROM sales_channels WHERE id = $1`, id)
}

func (r *PostgresRepository) GetBySlug(ctx interface{}, slug string) (*SalesChannel, error) {
	return r.scanOne(toContext(ctx), `SELECT id, name, slug, regions FROM sales_channels WHERE slug = $1`, slug)
}

func (r *PostgresRepository) scanOne(ctx context.Context, query, arg string) (*SalesChannel, error) {
	var ch SalesChannel
	var regionsJSON []byte
	err := r.db.QueryRowContext(ctx, query, arg).Scan(&ch.ID, &ch.Name, &ch.Slug, &regionsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("channel: get %s: %w", arg, err)
	}
	if len(regionsJSON) > 0 {
		_ = json.Unmarshal(regionsJSON, &ch.Regions)
	}
	return &ch, nil
}

func (r *PostgresRepository) List(ctx interface{}) ([]SalesChannel, error) {
	rows, err := r.db.QueryContext(toContext(ctx), `SELECT id, name, slug, regions FROM sales_channels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("channel: list: %w", err)
	}
	defer rows.Close()

	var result []SalesChannel
	for rows.Next() {
		var ch SalesChannel
		var regionsJSON []byte
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Slug, &regionsJSON); err != nil {
			return nil, fmt.Errorf("channel: scan: %w", err)
		}
		if len(regionsJSON) > 0 {
			_ = json.Unmarshal(regionsJSON, &ch.Regions)
		}
		result = append(result, ch)
	}
	return result, rows.Err()
}
