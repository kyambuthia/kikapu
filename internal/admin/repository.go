package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type IdempotencyStore interface {
	GetResponse(ctx context.Context, key string) ([]byte, int, error)
	SaveResponse(ctx context.Context, key string, statusCode int, response []byte) error
}

type PostgresProductRepository struct {
	db             *sql.DB
	idempotencyKey string
}

func NewPostgresProductRepository(db *sql.DB) *PostgresProductRepository {
	return &PostgresProductRepository{db: db}
}

func (r *PostgresProductRepository) Create(ctx context.Context, input ProductInput, idempotencyKey string) (*Product, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("admin: begin transaction: %w", err)
	}
	defer tx.Rollback()

	existingStatus, existingResponse, err := r.checkIdempotencyKey(ctx, tx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("admin: check idempotency key: %w", err)
	}

	if existingStatus > 0 {
		var product Product
		if err := json.Unmarshal(existingResponse, &product); err != nil {
			return nil, fmt.Errorf("admin: unmarshal idempotent response: %w", err)
		}
		return &product, nil
	}

	now := time.Now()
	productID := uuid.New().String()

	product := &Product{
		ID:        productID,
		Slug:      input.Slug,
		Name:      input.Name,
		Status:    input.Status,
		Variants:  make([]Variant, 0, len(input.Variants)),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if product.Status == "" {
		product.Status = StatusDraft
	}

	productQuery := `
		INSERT INTO products (id, slug, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err = tx.ExecContext(ctx, productQuery,
		product.ID, product.Slug, product.Name, product.Status, product.CreatedAt, product.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, &ValidationError{
				Message: "product with this slug already exists",
				Fields:  map[string]string{"slug": "product with this slug already exists"},
			}
		}
		return nil, fmt.Errorf("admin: insert product: %w", err)
	}

	for _, v := range input.Variants {
		variantID := uuid.New().String()
		variant := Variant{
			ID:        variantID,
			ProductID: product.ID,
			SKU:       v.SKU,
			Price:     money.NewAmount(v.Price, v.Currency),
			CreatedAt: now,
			UpdatedAt: now,
		}

		variantQuery := `
			INSERT INTO variants (id, product_id, sku, price, currency, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`

		_, err := tx.ExecContext(ctx, variantQuery,
			variant.ID, variant.ProductID, variant.SKU, variant.Price.Value, variant.Price.Currency,
			variant.CreatedAt, variant.UpdatedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, &ValidationError{
					Message: "variant with this SKU already exists",
					Fields:  map[string]string{"variants": "SKU already exists"},
				}
			}
			return nil, fmt.Errorf("admin: insert variant: %w", err)
		}

		product.Variants = append(product.Variants, variant)
	}

	responseJSON, err := json.Marshal(product)
	if err != nil {
		return nil, fmt.Errorf("admin: marshal product response: %w", err)
	}

	if err := r.saveIdempotencyKey(ctx, tx, idempotencyKey, 201, responseJSON); err != nil {
		return nil, fmt.Errorf("admin: save idempotency key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("admin: commit transaction: %w", err)
	}

	return product, nil
}

func (r *PostgresProductRepository) Get(ctx context.Context, id string) (*Product, error) {
	query := `
		SELECT id, slug, name, status, created_at, updated_at
		FROM products
		WHERE id = $1
	`

	var product Product
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&product.ID, &product.Slug, &product.Name, &product.Status,
		&product.CreatedAt, &product.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get product: %w", err)
	}

	variants, err := r.getVariants(ctx, product.ID)
	if err != nil {
		return nil, fmt.Errorf("admin: get variants: %w", err)
	}
	product.Variants = variants

	return &product, nil
}

func (r *PostgresProductRepository) GetBySlug(ctx context.Context, slug string) (*Product, error) {
	query := `
		SELECT id, slug, name, status, created_at, updated_at
		FROM products
		WHERE slug = $1
	`

	var product Product
	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&product.ID, &product.Slug, &product.Name, &product.Status,
		&product.CreatedAt, &product.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get product by slug: %w", err)
	}

	variants, err := r.getVariants(ctx, product.ID)
	if err != nil {
		return nil, fmt.Errorf("admin: get variants: %w", err)
	}
	product.Variants = variants

	return &product, nil
}

func (r *PostgresProductRepository) List(ctx context.Context, opts ProductListOptions) ([]Product, string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, "", ErrInvalidLimit
	}

	baseQuery := `
		SELECT p.id, p.slug, p.name, p.status, p.created_at, p.updated_at,
		       v.id, v.product_id, v.sku, v.price, v.currency, v.created_at, v.updated_at
		FROM products p
		LEFT JOIN variants v ON v.product_id = p.id
		WHERE 1=1
	`

	args := make([]interface{}, 0, 3)
	argNum := 1

	if opts.Status != "" {
		baseQuery += fmt.Sprintf(" AND p.status = $%d", argNum)
		args = append(args, opts.Status)
		argNum++
	}

	if opts.Cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.Cursor)
		if err != nil {
			return nil, "", ErrInvalidCursor
		}

		var cursorData struct {
			LastID string `json:"last_id"`
		}
		if err := json.Unmarshal(decoded, &cursorData); err != nil {
			return nil, "", ErrInvalidCursor
		}

		baseQuery += fmt.Sprintf(" AND p.id > $%d", argNum)
		args = append(args, cursorData.LastID)
		argNum++
	}

	baseQuery += fmt.Sprintf(" ORDER BY p.id, v.id LIMIT $%d", argNum)
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, "", fmt.Errorf("admin: query products: %w", err)
	}
	defer rows.Close()

	productMap := make(map[string]*Product)
	productOrder := make([]string, 0, limit)

	for rows.Next() {
		var productID, productSlug, productName string
		var productStatus ProductStatus
		var productCreatedAt, productUpdatedAt time.Time
		var variantID, variantProductID, variantSKU sql.NullString
		var variantPrice sql.NullInt64
		var variantCurrency sql.NullString
		var variantCreatedAt, variantUpdatedAt sql.NullTime

		if err := rows.Scan(
			&productID, &productSlug, &productName, &productStatus,
			&productCreatedAt, &productUpdatedAt,
			&variantID, &variantProductID, &variantSKU, &variantPrice,
			&variantCurrency, &variantCreatedAt, &variantUpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("admin: scan product: %w", err)
		}

		if _, exists := productMap[productID]; !exists {
			productMap[productID] = &Product{
				ID:        productID,
				Slug:      productSlug,
				Name:      productName,
				Status:    productStatus,
				Variants:  []Variant{},
				CreatedAt: productCreatedAt,
				UpdatedAt: productUpdatedAt,
			}
			productOrder = append(productOrder, productID)
		}

		if variantID.Valid {
			variant := Variant{
				ID:        variantID.String,
				ProductID: variantProductID.String,
				SKU:       variantSKU.String,
				Price:     money.NewAmount(variantPrice.Int64, variantCurrency.String),
				CreatedAt: variantCreatedAt.Time,
				UpdatedAt: variantUpdatedAt.Time,
			}
			productMap[productID].Variants = append(productMap[productID].Variants, variant)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("admin: iterate products: %w", err)
	}

	products := make([]Product, 0, len(productOrder))
	for _, id := range productOrder {
		products = append(products, *productMap[id])
	}

	var nextCursor string
	if len(products) > limit {
		products = products[:limit]
		lastID := products[len(products)-1].ID
		cursorData := struct {
			LastID string `json:"last_id"`
		}{LastID: lastID}
		cursorBytes, err := json.Marshal(cursorData)
		if err != nil {
			return nil, "", fmt.Errorf("admin: marshal cursor: %w", err)
		}
		nextCursor = base64.StdEncoding.EncodeToString(cursorBytes)
	}

	return products, nextCursor, nil
}

func (r *PostgresProductRepository) Update(ctx context.Context, id string, input ProductInput) (*Product, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("admin: begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()

	query := `
		UPDATE products
		SET slug = $1, name = $2, status = $3, updated_at = $4
		WHERE id = $5
	`

	result, err := tx.ExecContext(ctx, query, input.Slug, input.Name, input.Status, now, id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, &ValidationError{
				Message: "product with this slug already exists",
				Fields:  map[string]string{"slug": "product with this slug already exists"},
			}
		}
		return nil, fmt.Errorf("admin: update product: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("admin: get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, ErrProductNotFound
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM variants WHERE product_id = $1", id)
	if err != nil {
		return nil, fmt.Errorf("admin: delete old variants: %w", err)
	}

	variants := make([]Variant, 0, len(input.Variants))
	for _, v := range input.Variants {
		variantID := uuid.New().String()
		variant := Variant{
			ID:        variantID,
			ProductID: id,
			SKU:       v.SKU,
			Price:     money.NewAmount(v.Price, v.Currency),
			CreatedAt: now,
			UpdatedAt: now,
		}

		variantQuery := `
			INSERT INTO variants (id, product_id, sku, price, currency, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`

		_, err := tx.ExecContext(ctx, variantQuery,
			variant.ID, variant.ProductID, variant.SKU, variant.Price.Value, variant.Price.Currency,
			variant.CreatedAt, variant.UpdatedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, &ValidationError{
					Message: "variant with this SKU already exists",
					Fields:  map[string]string{"variants": "SKU already exists"},
				}
			}
			return nil, fmt.Errorf("admin: insert variant: %w", err)
		}

		variants = append(variants, variant)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("admin: commit transaction: %w", err)
	}

	return &Product{
		ID:        id,
		Slug:      input.Slug,
		Name:      input.Name,
		Status:    input.Status,
		Variants:  variants,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (r *PostgresProductRepository) Delete(ctx context.Context, id string) error {
	hasOrders, err := r.HasOrders(ctx, id)
	if err != nil {
		return fmt.Errorf("admin: check product orders: %w", err)
	}

	if hasOrders {
		return ErrProductInUse
	}

	query := `
		UPDATE products
		SET status = 'archived', updated_at = $1
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("admin: delete product: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("admin: get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrProductNotFound
	}

	return nil
}

func (r *PostgresProductRepository) HasOrders(ctx context.Context, productID string) (bool, error) {
	query := `
		SELECT COUNT(*) > 0
		FROM order_lines ol
		INNER JOIN variants v ON v.id::text = ol.variant_id
		WHERE v.product_id = $1
	`

	var hasOrders bool
	err := r.db.QueryRowContext(ctx, query, productID).Scan(&hasOrders)
	if err != nil {
		return false, fmt.Errorf("admin: check orders: %w", err)
	}

	return hasOrders, nil
}

func (r *PostgresProductRepository) getVariants(ctx context.Context, productID string) ([]Variant, error) {
	query := `
		SELECT id, product_id, sku, price, currency, created_at, updated_at
		FROM variants
		WHERE product_id = $1
		ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("admin: query variants: %w", err)
	}

	var variants []Variant
	for rows.Next() {
		var v Variant
		var currency string
		if err := rows.Scan(
			&v.ID, &v.ProductID, &v.SKU, &v.Price.Value, currency,
			&v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("admin: scan variant: %w", err)
		}
		v.Price.Currency = currency
		variants = append(variants, v)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("admin: iterate variants: %w", err)
	}

	rows.Close()
	return variants, nil
}

func (r *PostgresProductRepository) checkIdempotencyKey(ctx context.Context, tx *sql.Tx, key string) (int, []byte, error) {
	keyHash := sha256.Sum256([]byte(key))

	query := `
		SELECT status_code, response_body
		FROM idempotency_keys
		WHERE key_hash = $1
	`

	var statusCode int
	var responseBody []byte
	err := tx.QueryRowContext(ctx, query, keyHash[:]).Scan(&statusCode, &responseBody)
	if err == sql.ErrNoRows {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}

	return statusCode, responseBody, nil
}

func (r *PostgresProductRepository) saveIdempotencyKey(ctx context.Context, tx *sql.Tx, key string, statusCode int, response []byte) error {
	keyHash := sha256.Sum256([]byte(key))

	query := `
		INSERT INTO idempotency_keys (key_hash, status_code, response_body, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key_hash) DO NOTHING
	`

	_, err := tx.ExecContext(ctx, query, keyHash[:], statusCode, response, time.Now())
	return err
}

type PostgresStockRepository struct {
	db *sql.DB
}

func NewPostgresStockRepository(db *sql.DB) *PostgresStockRepository {
	return &PostgresStockRepository{db: db}
}

func (r *PostgresStockRepository) GetLevel(ctx context.Context, variantID, locationID string) (*StockLevel, error) {
	query := `
		SELECT variant_id, location_id, on_hand, reserved
		FROM stock_levels
		WHERE variant_id = $1 AND location_id = $2
	`

	var level StockLevel
	err := r.db.QueryRowContext(ctx, query, variantID, locationID).Scan(
		&level.VariantID, &level.LocationID, &level.OnHand, &level.Reserved,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get stock level: %w", err)
	}

	return &level, nil
}

func (r *PostgresStockRepository) SetLevel(ctx context.Context, update StockUpdate) (*StockLevel, error) {
	query := `
		INSERT INTO stock_levels (variant_id, location_id, on_hand, reserved, created_at, updated_at)
		VALUES ($1, $2, $3, 0, $4, $4)
		ON CONFLICT (variant_id, location_id)
		DO UPDATE SET on_hand = $3, updated_at = $4
		RETURNING variant_id, location_id, on_hand, reserved
	`

	now := time.Now()
	var level StockLevel
	err := r.db.QueryRowContext(ctx, query,
		update.VariantID, update.LocationID, update.OnHand, now,
	).Scan(&level.VariantID, &level.LocationID, &level.OnHand, &level.Reserved)
	if err != nil {
		return nil, fmt.Errorf("admin: set stock level: %w", err)
	}

	return &level, nil
}

func (r *PostgresStockRepository) UpdateLevel(ctx context.Context, variantID, locationID string, delta int) (*StockLevel, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("admin: begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		SELECT on_hand, reserved
		FROM stock_levels
		WHERE variant_id = $1 AND location_id = $2
		FOR UPDATE
	`

	var currentOnHand, currentReserved int
	err = tx.QueryRowContext(ctx, query, variantID, locationID).Scan(&currentOnHand, &currentReserved)
	if err == sql.ErrNoRows {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get current stock: %w", err)
	}

	newOnHand := currentOnHand + delta
	if newOnHand < currentReserved {
		return nil, ErrInsufficientStock
	}

	updateQuery := `
		UPDATE stock_levels
		SET on_hand = $1, updated_at = $2
		WHERE variant_id = $3 AND location_id = $4
		RETURNING variant_id, location_id, on_hand, reserved
	`

	var level StockLevel
	err = tx.QueryRowContext(ctx, updateQuery, newOnHand, time.Now(), variantID, locationID).Scan(
		&level.VariantID, &level.LocationID, &level.OnHand, &level.Reserved,
	)
	if err != nil {
		return nil, fmt.Errorf("admin: update stock: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("admin: commit transaction: %w", err)
	}

	return &level, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) ||
		strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "UNIQUE constraint"))
}
