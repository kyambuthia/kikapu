package search

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidQuery  = errors.New("search: invalid query")
	ErrInvalidLimit  = errors.New("search: invalid limit")
	ErrInvalidCursor = errors.New("search: invalid cursor")
)

type Filters struct {
	Status  string
	Channel string
}

type Page struct {
	Limit  int
	Cursor string
}

type Result struct {
	ID       string    `json:"id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Rank     float64   `json:"rank"`
	Variants []Variant `json:"variants"`
}

type Variant struct {
	ID       string       `json:"id"`
	SKU      string       `json:"sku"`
	Price    money.Amount `json:"price"`
	Currency string       `json:"currency"`
}

type Results struct {
	Data       []Result `json:"data"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Query(ctx context.Context, q string, filters Filters, page Page) (*Results, error) {
	if q == "" {
		return nil, ErrInvalidQuery
	}

	limit := page.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		return nil, ErrInvalidLimit
	}

	var lastRank *float64
	var lastID *string
	if page.Cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(page.Cursor)
		if err != nil {
			return nil, ErrInvalidCursor
		}
		var cursor struct {
			Rank float64 `json:"rank"`
			ID   string  `json:"id"`
		}
		if err := json.Unmarshal(decoded, &cursor); err != nil {
			return nil, ErrInvalidCursor
		}
		lastRank = &cursor.Rank
		lastID = &cursor.ID
	}

	tsquery := "websearch_to_tsquery('english', $1)"

	query := fmt.Sprintf(`
		WITH fts_results AS (
			SELECT 
				p.id, p.slug, p.name, p.status,
				ts_rank_cd(p.search, %s) AS rank
			FROM products p
			WHERE p.search @@ %s
				AND ($2::text IS NULL OR p.status = $2)
			ORDER BY rank DESC, p.id
			LIMIT $3
		),
		trgm_results AS (
			SELECT 
				p.id, p.slug, p.name, p.status,
				word_similarity($1, p.name) AS rank
			FROM products p
			WHERE word_similarity($1, p.name) > 0.3
				AND ($2::text IS NULL OR p.status = $2)
				AND p.id NOT IN (SELECT id FROM fts_results)
			ORDER BY rank DESC, p.id
			LIMIT $3
		),
		combined AS (
			SELECT * FROM fts_results
			UNION ALL
			SELECT * FROM trgm_results
			ORDER BY rank DESC, id
			LIMIT $3
		)
		SELECT id, slug, name, status, rank
		FROM combined
		WHERE ($4::float IS NULL OR $5::text IS NULL OR rank < $4 OR (rank = $4 AND id::text > $5))
		ORDER BY rank DESC, id
	`, tsquery, tsquery)

	var statusFilter any
	if filters.Status != "" {
		statusFilter = filters.Status
	}

	rows, err := s.db.QueryContext(ctx, query, q, statusFilter, limit+1, lastRank, lastID)
	if err != nil {
		return nil, fmt.Errorf("search: query failed: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Slug, &r.Name, &r.Status, &r.Rank); err != nil {
			return nil, fmt.Errorf("search: scan result: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate results: %w", err)
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[len(results)-1]
		cursorData := struct {
			Rank float64 `json:"rank"`
			ID   string  `json:"id"`
		}{
			Rank: last.Rank,
			ID:   last.ID,
		}
		cursorBytes, err := json.Marshal(cursorData)
		if err != nil {
			return nil, fmt.Errorf("search: encode cursor: %w", err)
		}
		nextCursor = base64.StdEncoding.EncodeToString(cursorBytes)
	}

	for i := range results {
		variants, err := s.getVariants(ctx, results[i].ID)
		if err != nil {
			return nil, fmt.Errorf("search: get variants for product %s: %w", results[i].ID, err)
		}
		results[i].Variants = variants
	}

	return &Results{
		Data:       results,
		NextCursor: nextCursor,
	}, nil
}

func (s *PostgresStore) getVariants(ctx context.Context, productID string) ([]Variant, error) {
	query := `
		SELECT id, sku, price, currency
		FROM variants
		WHERE product_id = $1
		ORDER BY created_at
	`

	rows, err := s.db.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("get variants: %w", err)
	}
	defer rows.Close()

	var variants []Variant
	for rows.Next() {
		var v Variant
		if err := rows.Scan(&v.ID, &v.SKU, &v.Price.Value, &v.Price.Currency); err != nil {
			return nil, fmt.Errorf("scan variant: %w", err)
		}
		v.Currency = v.Price.Currency
		variants = append(variants, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate variant rows: %w", err)
	}

	return variants, nil
}
