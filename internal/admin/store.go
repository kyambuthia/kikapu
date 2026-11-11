package admin

import (
	"context"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/order"
)

type IdempotencyStore interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
}

type InMemoryIdempotencyStore struct {
	mu    sync.RWMutex
	store map[string]interface{}
}

func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		store: make(map[string]interface{}),
	}
}

func (s *InMemoryIdempotencyStore) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.store[key]
	return val, ok
}

func (s *InMemoryIdempotencyStore) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = value
}

type ProductRepository interface {
	Create(ctx context.Context, product *catalog.Product) error
	Update(ctx context.Context, product *catalog.Product) error
	Delete(ctx context.Context, id string) error
	GetByID(ctx context.Context, id string) (*catalog.Product, error)
	GetBySlug(ctx context.Context, slug string) (*catalog.Product, error)
	List(ctx context.Context, status string, cursor string, limit int) ([]catalog.Product, string, error)
	HasOrders(ctx context.Context, productID string) (bool, error)
}

type InventoryRepository interface {
	SetLevel(ctx context.Context, variantID, locationID string, onHand int) error
	GetLevel(ctx context.Context, variantID, locationID string) (*inventory.Level, error)
	GetLevels(ctx context.Context, variantID string) ([]inventory.Level, error)
}

type OrderRepository interface {
	List(ctx context.Context, status string, from, to time.Time, cursor string, limit int) ([]order.Order, string, error)
	Get(ctx context.Context, id string) (*order.Order, error)
}

type InMemoryProductRepository struct {
	products map[string]*catalog.Product
	bySlug   map[string]*catalog.Product
	mu       sync.RWMutex
}

func NewInMemoryProductRepository() *InMemoryProductRepository {
	return &InMemoryProductRepository{
		products: make(map[string]*catalog.Product),
		bySlug:   make(map[string]*catalog.Product),
	}
}

func (r *InMemoryProductRepository) Create(ctx context.Context, product *catalog.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.bySlug[product.Slug]; exists {
		return ErrDuplicateSlug
	}

	r.products[product.ID] = product
	r.bySlug[product.Slug] = product
	return nil
}

func (r *InMemoryProductRepository) Update(ctx context.Context, product *catalog.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, exists := r.products[product.ID]
	if !exists {
		return ErrProductNotFound
	}

	if product.Slug != existing.Slug {
		if _, exists := r.bySlug[product.Slug]; exists {
			return ErrDuplicateSlug
		}
		delete(r.bySlug, existing.Slug)
		r.bySlug[product.Slug] = product
	}

	r.products[product.ID] = product
	return nil
}

func (r *InMemoryProductRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	product, exists := r.products[id]
	if !exists {
		return ErrProductNotFound
	}

	product.Status = "archived"
	return nil
}

func (r *InMemoryProductRepository) GetByID(ctx context.Context, id string) (*catalog.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	product, exists := r.products[id]
	if !exists {
		return nil, ErrProductNotFound
	}

	return product, nil
}

func (r *InMemoryProductRepository) GetBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	product, exists := r.bySlug[slug]
	if !exists {
		return nil, ErrProductNotFound
	}

	return product, nil
}

func (r *InMemoryProductRepository) List(ctx context.Context, status string, cursor string, limit int) ([]catalog.Product, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var products []catalog.Product
	for _, p := range r.products {
		if status == "" || p.Status == status {
			products = append(products, *p)
		}
	}

	result := make([]catalog.Product, 0, limit)
	var nextCursor string

	for i, p := range products {
		if cursor != "" && p.ID <= cursor {
			continue
		}
		if len(result) >= limit {
			nextCursor = result[len(result)-1].ID
			break
		}
		result = append(result, p)
		_ = i
	}

	return result, nextCursor, nil
}

func (r *InMemoryProductRepository) HasOrders(ctx context.Context, productID string) (bool, error) {
	return false, nil
}

type InMemoryInventoryRepository struct {
	levels map[string]map[string]*inventory.Level
	mu     sync.RWMutex
}

func NewInMemoryInventoryRepository() *InMemoryInventoryRepository {
	return &InMemoryInventoryRepository{
		levels: make(map[string]map[string]*inventory.Level),
	}
}

func (r *InMemoryInventoryRepository) SetLevel(ctx context.Context, variantID, locationID string, onHand int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.levels[variantID] == nil {
		r.levels[variantID] = make(map[string]*inventory.Level)
	}

	r.levels[variantID][locationID] = &inventory.Level{
		VariantID:  variantID,
		LocationID: locationID,
		OnHand:     onHand,
		Reserved:   0,
	}
	return nil
}

func (r *InMemoryInventoryRepository) GetLevel(ctx context.Context, variantID, locationID string) (*inventory.Level, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.levels[variantID] == nil {
		return nil, nil
	}

	level, exists := r.levels[variantID][locationID]
	if !exists {
		return nil, nil
	}

	return level, nil
}

func (r *InMemoryInventoryRepository) GetLevels(ctx context.Context, variantID string) ([]inventory.Level, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.levels[variantID] == nil {
		return nil, nil
	}

	levels := make([]inventory.Level, 0, len(r.levels[variantID]))
	for _, l := range r.levels[variantID] {
		levels = append(levels, *l)
	}
	return levels, nil
}

type InMemoryOrderRepository struct {
	orders map[string]*order.Order
	mu     sync.RWMutex
}

func NewInMemoryOrderRepository() *InMemoryOrderRepository {
	return &InMemoryOrderRepository{
		orders: make(map[string]*order.Order),
	}
}

func (r *InMemoryOrderRepository) AddOrder(o *order.Order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[o.ID] = o
}

func (r *InMemoryOrderRepository) List(ctx context.Context, status string, from, to time.Time, cursor string, limit int) ([]order.Order, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var orders []order.Order
	for _, o := range r.orders {
		if status != "" && string(o.Status) != status {
			continue
		}
		if !from.IsZero() && o.CreatedAt.Before(from) {
			continue
		}
		if !to.IsZero() && o.CreatedAt.After(to) {
			continue
		}
		orders = append(orders, *o)
	}

	result := make([]order.Order, 0, limit)
	var nextCursor string

	for _, o := range orders {
		if cursor != "" && o.ID <= cursor {
			continue
		}
		if len(result) >= limit {
			nextCursor = result[len(result)-1].ID
			break
		}
		result = append(result, o)
	}

	return result, nextCursor, nil
}

func (r *InMemoryOrderRepository) Get(ctx context.Context, id string) (*order.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	o, exists := r.orders[id]
	if !exists {
		return nil, order.ErrOrderNotFound
	}

	return o, nil
}
