package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx interface{}, slug string) (*Product, error) {
	c := toContext(ctx)
	query := `
		SELECT p.id, p.slug, p.name, p.status,
		       v.id, v.sku, v.price, v.currency
		FROM products p
		LEFT JOIN variants v ON v.product_id = p.id
		WHERE p.slug = $1
		ORDER BY v.created_at
	`

	rows, err := s.db.QueryContext(c, query, slug)
	if err != nil {
		return nil, fmt.Errorf("catalog: get product %s: %w", slug, err)
	}
	defer rows.Close()

	var product Product
	var variants []Variant
	hasProduct := false

	for rows.Next() {
		hasProduct = true
		var (
			pID, pSlug, pName, pStatus string
			vID, vSku                  sql.NullString
			vPrice                     sql.NullInt64
			vCurrency                  sql.NullString
		)

		err := rows.Scan(&pID, &pSlug, &pName, &pStatus, &vID, &vSku, &vPrice, &vCurrency)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan product: %w", err)
		}

		product.ID = pID
		product.Slug = pSlug
		product.Name = pName
		product.Status = pStatus

		if vID.Valid {
			variants = append(variants, Variant{
				ID:       vID.String,
				SKU:      vSku.String,
				Price:    money.NewAmount(vPrice.Int64, vCurrency.String),
				Currency: vCurrency.String,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: iterate product rows: %w", err)
	}

	if !hasProduct {
		return nil, ErrProductNotFound
	}

	product.Variants = variants
	return &product, nil
}

func (s *PostgresStore) List(ctx interface{}, opts ListOptions) ([]Product, string, error) {
	c := toContext(ctx)
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, "", ErrInvalidLimit
	}

	var afterID string
	if opts.Cursor != "" {
		var err error
		afterID, err = decodeCursor(opts.Cursor)
		if err != nil {
			return nil, "", err
		}
	}

	var (
		rows *sql.Rows
		err  error
	)
	if afterID != "" {
		query := `
			SELECT p.id, p.slug, p.name, p.status
			FROM products p
			WHERE p.id > $1
			ORDER BY p.id
			LIMIT $2
		`
		rows, err = s.db.QueryContext(c, query, afterID, limit+1)
	} else {
		query := `
			SELECT p.id, p.slug, p.name, p.status
			FROM products p
			ORDER BY p.id
			LIMIT $1
		`
		rows, err = s.db.QueryContext(c, query, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("catalog: list products: %w", err)
	}
	defer rows.Close()

	var products []Product

	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Status); err != nil {
			return nil, "", fmt.Errorf("catalog: scan product: %w", err)
		}
		products = append(products, p)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("catalog: iterate product rows: %w", err)
	}

	var nextCursor string
	if len(products) > limit {
		products = products[:limit]
		var err error
		nextCursor, err = encodeCursor(products[limit-1].ID)
		if err != nil {
			return nil, "", fmt.Errorf("catalog: marshal cursor: %w", err)
		}
	}

	for i := range products {
		variants, err := s.getVariants(c, products[i].ID)
		if err != nil {
			return nil, "", fmt.Errorf("catalog: get variants for product %s: %w", products[i].ID, err)
		}
		products[i].Variants = variants
	}

	return products, nextCursor, nil
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

func toContext(ctx interface{}) context.Context {
	if c, ok := ctx.(context.Context); ok {
		return c
	}
	return context.Background()
}
