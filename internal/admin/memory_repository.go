package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type MemoryProductRepository struct {
	products    map[string]*Product
	bySlug      map[string]*Product
	idempotency map[string]*idempotencyRecord
	mu          sync.RWMutex
	keyGen      func() string
}

type idempotencyRecord struct {
	statusCode int
	response   []byte
}

func NewMemoryProductRepository() *MemoryProductRepository {
	return &MemoryProductRepository{
		products:    make(map[string]*Product),
		bySlug:      make(map[string]*Product),
		idempotency: make(map[string]*idempotencyRecord),
		keyGen: func() string {
			return time.Now().Format("20060102150405.999999999")
		},
	}
}

func (r *MemoryProductRepository) Create(ctx context.Context, input ProductInput, idempotencyKey string) (*Product, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if record, ok := r.idempotency[idempotencyKey]; ok {
		var product Product
		if err := product.unmarshalJSON(record.response); err == nil {
			return &product, nil
		}
	}

	if _, exists := r.bySlug[input.Slug]; exists {
		return nil, &ValidationError{
			Message: "product with this slug already exists",
			Fields:  map[string]string{"slug": "product with this slug already exists"},
		}
	}

	now := time.Now()
	product := &Product{
		ID:        r.keyGen(),
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

	skus := make(map[string]bool)
	for _, v := range input.Variants {
		if skus[v.SKU] {
			return nil, &ValidationError{
				Message: "duplicate SKU",
				Fields:  map[string]string{"variants": "duplicate SKU"},
			}
		}
		skus[v.SKU] = true

		variant := Variant{
			ID:        r.keyGen(),
			ProductID: product.ID,
			SKU:       v.SKU,
			Price:     money.NewAmount(v.Price, v.Currency),
			CreatedAt: now,
			UpdatedAt: now,
		}
		product.Variants = append(product.Variants, variant)
	}

	r.products[product.ID] = product
	r.bySlug[product.Slug] = product

	responseJSON := product.marshalJSON()
	r.idempotency[idempotencyKey] = &idempotencyRecord{
		statusCode: 201,
		response:   responseJSON,
	}

	return product, nil
}

func (r *MemoryProductRepository) Get(ctx context.Context, id string) (*Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	product, ok := r.products[id]
	if !ok {
		return nil, ErrProductNotFound
	}

	return product, nil
}

func (r *MemoryProductRepository) GetBySlug(ctx context.Context, slug string) (*Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	product, ok := r.bySlug[slug]
	if !ok {
		return nil, ErrProductNotFound
	}

	return product, nil
}

func (r *MemoryProductRepository) List(ctx context.Context, opts ProductListOptions) ([]Product, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, "", ErrInvalidLimit
	}

	var products []Product
	for _, p := range r.products {
		if opts.Status != "" && p.Status != opts.Status {
			continue
		}
		products = append(products, *p)
	}

	if len(products) == 0 {
		return []Product{}, "", nil
	}

	startIdx := 0
	if opts.Cursor != "" {
		for i, p := range products {
			if p.ID > opts.Cursor {
				startIdx = i
				break
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(products) {
		endIdx = len(products)
	}

	result := products[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(products) {
		nextCursor = result[len(result)-1].ID
	}

	return result, nextCursor, nil
}

func (r *MemoryProductRepository) Update(ctx context.Context, id string, input ProductInput) (*Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	product, ok := r.products[id]
	if !ok {
		return nil, ErrProductNotFound
	}

	delete(r.bySlug, product.Slug)

	if _, exists := r.bySlug[input.Slug]; exists && input.Slug != product.Slug {
		return nil, &ValidationError{
			Message: "product with this slug already exists",
			Fields:  map[string]string{"slug": "product with this slug already exists"},
		}
	}

	now := time.Now()
	product.Slug = input.Slug
	product.Name = input.Name
	product.Status = input.Status
	product.UpdatedAt = now

	skus := make(map[string]bool)
	product.Variants = make([]Variant, 0, len(input.Variants))
	for _, v := range input.Variants {
		if skus[v.SKU] {
			return nil, &ValidationError{
				Message: "duplicate SKU",
				Fields:  map[string]string{"variants": "duplicate SKU"},
			}
		}
		skus[v.SKU] = true

		variant := Variant{
			ID:        r.keyGen(),
			ProductID: product.ID,
			SKU:       v.SKU,
			Price:     money.NewAmount(v.Price, v.Currency),
			CreatedAt: now,
			UpdatedAt: now,
		}
		product.Variants = append(product.Variants, variant)
	}

	r.bySlug[product.Slug] = product

	return product, nil
}

func (r *MemoryProductRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	product, ok := r.products[id]
	if !ok {
		return ErrProductNotFound
	}

	product.Status = StatusArchived
	product.UpdatedAt = time.Now()

	return nil
}

func (r *MemoryProductRepository) HasOrders(ctx context.Context, productID string) (bool, error) {
	return false, nil
}

func (p *Product) marshalJSON() []byte {
	data := map[string]interface{}{
		"id":         p.ID,
		"slug":       p.Slug,
		"name":       p.Name,
		"status":     p.Status,
		"variants":   p.Variants,
		"created_at": p.CreatedAt,
		"updated_at": p.UpdatedAt,
	}
	jsonBytes, _ := json.Marshal(data)
	return jsonBytes
}

func (p *Product) unmarshalJSON(data []byte) error {
	var tmp struct {
		ID     string `json:"id"`
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	p.ID = tmp.ID
	p.Slug = tmp.Slug
	p.Name = tmp.Name
	p.Status = ProductStatus(tmp.Status)
	return nil
}

type MemoryStockRepository struct {
	levels map[string]*StockLevel
	mu     sync.RWMutex
}

func NewMemoryStockRepository() *MemoryStockRepository {
	return &MemoryStockRepository{
		levels: make(map[string]*StockLevel),
	}
}

func key(variantID, locationID string) string {
	return variantID + ":" + locationID
}

func (r *MemoryStockRepository) GetLevel(ctx context.Context, variantID, locationID string) (*StockLevel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	level, ok := r.levels[key(variantID, locationID)]
	if !ok {
		return nil, nil
	}

	return level, nil
}

func (r *MemoryStockRepository) SetLevel(ctx context.Context, update StockUpdate) (*StockLevel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := key(update.VariantID, update.LocationID)
	level := &StockLevel{
		VariantID:  update.VariantID,
		LocationID: update.LocationID,
		OnHand:     update.OnHand,
		Reserved:   0,
	}

	if existing, ok := r.levels[k]; ok {
		level.Reserved = existing.Reserved
	}

	r.levels[k] = level

	return level, nil
}

func (r *MemoryStockRepository) UpdateLevel(ctx context.Context, variantID, locationID string, delta int) (*StockLevel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	level, ok := r.levels[key(variantID, locationID)]
	if !ok {
		return nil, ErrProductNotFound
	}

	newOnHand := level.OnHand + delta
	if newOnHand < level.Reserved {
		return nil, ErrInsufficientStock
	}

	level.OnHand = newOnHand

	return level, nil
}

type MemoryOrderLister struct {
	mu     sync.RWMutex
	orders map[string]Order
}

func NewMemoryOrderLister(orders ...Order) *MemoryOrderLister {
	m := make(map[string]Order, len(orders))
	for _, o := range orders {
		m[o.ID] = o
	}
	return &MemoryOrderLister{orders: m}
}

func (l *MemoryOrderLister) List(ctx context.Context, opts OrderListOptions) ([]Order, string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	result := make([]Order, 0, len(l.orders))
	for _, o := range l.orders {
		if opts.Status != "" && o.Status != opts.Status {
			continue
		}
		if !opts.From.IsZero() && o.CreatedAt.Before(opts.From) {
			continue
		}
		if !opts.To.IsZero() && o.CreatedAt.After(opts.To) {
			continue
		}
		result = append(result, o)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	start := 0
	if opts.Cursor != "" {
		for i, o := range result {
			if o.ID > opts.Cursor {
				start = i
				break
			}
			start = len(result)
		}
	}

	end := start + limit
	if end > len(result) {
		end = len(result)
	}

	var nextCursor string
	if end < len(result) {
		nextCursor = result[end-1].ID
	}

	return result[start:end], nextCursor, nil
}

func (l *MemoryOrderLister) Get(ctx context.Context, id string) (*Order, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	o, ok := l.orders[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &o, nil
}
