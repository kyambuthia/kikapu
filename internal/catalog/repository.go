package catalog

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

type InMemoryRepository struct {
	products map[string]*Product
	bySlug   map[string]*Product
	mu       sync.RWMutex
}

func NewInMemoryRepository(products []Product) *InMemoryRepository {
	repo := &InMemoryRepository{
		products: make(map[string]*Product),
		bySlug:   make(map[string]*Product),
	}
	for _, p := range products {
		product := p
		repo.products[product.ID] = &product
		repo.bySlug[product.Slug] = &product
	}
	return repo
}

func (r *InMemoryRepository) Get(ctx interface{}, slug string) (*Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	product, ok := r.bySlug[slug]
	if !ok {
		return nil, ErrProductNotFound
	}
	return product, nil
}

func (r *InMemoryRepository) List(ctx interface{}, opts ListOptions) ([]Product, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, "", ErrInvalidLimit
	}

	ids := make([]string, 0, len(r.products))
	for id := range r.products {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	startIdx := 0
	if opts.Cursor != "" {
		lastID, err := decodeCursor(opts.Cursor)
		if err != nil {
			return nil, "", err
		}

		found := false
		for i, id := range ids {
			if id > lastID {
				startIdx = i
				found = true
				break
			}
		}
		if !found {
			startIdx = len(ids)
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(ids) {
		endIdx = len(ids)
	}

	result := make([]Product, 0, limit)
	for i := startIdx; i < endIdx; i++ {
		product := r.products[ids[i]]
		result = append(result, *product)
	}

	var nextCursor string
	if endIdx < len(ids) {
		var err error
		nextCursor, err = encodeCursor(ids[endIdx-1])
		if err != nil {
			return nil, "", err
		}
	}

	return result, nextCursor, nil
}

func decodeCursor(cursor string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", ErrInvalidCursor
	}
	var cursorData struct {
		LastID string `json:"last_id"`
	}
	if err := json.Unmarshal(decoded, &cursorData); err != nil {
		return "", ErrInvalidCursor
	}
	return cursorData.LastID, nil
}

func encodeCursor(lastID string) (string, error) {
	cursorData := struct {
		LastID string `json:"last_id"`
	}{LastID: lastID}
	cursorBytes, err := json.Marshal(cursorData)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(cursorBytes), nil
}

func LoadProductsFromJSON(data []byte) ([]Product, error) {
	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, err
	}
	return products, nil
}

func ValidateProducts(products []Product) error {
	for _, p := range products {
		if err := ValidateSlug(p.Slug); err != nil {
			return err
		}
		if p.ID == "" {
			return errors.New("product ID cannot be empty")
		}
		if p.Name == "" {
			return errors.New("product name cannot be empty")
		}
	}
	return nil
}
