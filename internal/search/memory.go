package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"github.com/kyambuthia/kikapu/internal/catalog"
)

// InMemoryStore implements product search over a catalog.Repository for
// local development and environments without PostgreSQL.
type InMemoryStore struct {
	repo catalog.Repository
}

func NewInMemoryStore(repo catalog.Repository) *InMemoryStore {
	return &InMemoryStore{repo: repo}
}

func (s *InMemoryStore) Query(ctx context.Context, q string, filters Filters, page Page) (*Results, error) {
	if strings.TrimSpace(q) == "" {
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

	products, err := s.loadAllProducts(ctx)
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(q))
	var matched []Result
	for _, p := range products {
		if filters.Status != "" && p.Status != filters.Status {
			continue
		}
		rank := scoreProduct(p, terms)
		if rank <= 0 {
			continue
		}
		variants := make([]Variant, 0, len(p.Variants))
		for _, v := range p.Variants {
			variants = append(variants, Variant{
				ID:       v.ID,
				SKU:      v.SKU,
				Price:    v.Price,
				Currency: v.Currency,
			})
		}
		matched = append(matched, Result{
			ID:       p.ID,
			Slug:     p.Slug,
			Name:     p.Name,
			Status:   p.Status,
			Rank:     rank,
			Variants: variants,
		})
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Rank != matched[j].Rank {
			return matched[i].Rank > matched[j].Rank
		}
		return matched[i].ID < matched[j].ID
	})

	if lastRank != nil && lastID != nil {
		filtered := matched[:0]
		for _, r := range matched {
			if r.Rank < *lastRank || (r.Rank == *lastRank && r.ID > *lastID) {
				filtered = append(filtered, r)
			}
		}
		matched = filtered
	}

	var nextCursor string
	if len(matched) > limit {
		matched = matched[:limit]
		last := matched[len(matched)-1]
		b, err := json.Marshal(struct {
			Rank float64 `json:"rank"`
			ID   string  `json:"id"`
		}{Rank: last.Rank, ID: last.ID})
		if err == nil {
			nextCursor = base64.StdEncoding.EncodeToString(b)
		}
	}

	if matched == nil {
		matched = []Result{}
	}

	return &Results{
		Data:       matched,
		NextCursor: nextCursor,
	}, nil
}

func (s *InMemoryStore) loadAllProducts(ctx context.Context) ([]catalog.Product, error) {
	if s.repo == nil {
		return nil, nil
	}
	var all []catalog.Product
	var cursor string
	for {
		batch, next, err := s.repo.List(ctx, catalog.ListOptions{
			Limit:  catalog.MaxLimit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if next == "" {
			break
		}
		cursor = next
	}
	return all, nil
}

func scoreProduct(p catalog.Product, terms []string) float64 {
	nameLower := strings.ToLower(p.Name)
	slugLower := strings.ToLower(p.Slug)
	var skuBuilder strings.Builder
	for _, v := range p.Variants {
		skuBuilder.WriteString(strings.ToLower(v.SKU))
		skuBuilder.WriteString(" ")
	}
	skusLower := skuBuilder.String()

	var score float64
	for _, term := range terms {
		switch {
		case nameLower == term || slugLower == term:
			score += 3.0
		case strings.Contains(nameLower, term):
			score += 2.0
		case strings.Contains(slugLower, term) || strings.Contains(skusLower, term):
			score += 1.0
		default:
			return 0
		}
	}
	return score
}
