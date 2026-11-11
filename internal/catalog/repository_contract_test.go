package catalog

import (
	"context"
	"testing"
)

type RepositoryContract struct {
	NewRepo func(t *testing.T) Repository
}

func (c *RepositoryContract) Run(t *testing.T) {
	t.Run("Get", c.testGet)
	t.Run("List", c.testList)
	t.Run("Pagination", c.testPagination)
}

func (c *RepositoryContract) testGet(t *testing.T) {
	ctx := context.Background()
	repo := c.NewRepo(t)

	t.Run("found", func(t *testing.T) {
		product, err := repo.Get(ctx, "wireless-mouse")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if product.Slug != "wireless-mouse" {
			t.Errorf("Get() slug = %v, want wireless-mouse", product.Slug)
		}
		if len(product.Variants) == 0 {
			t.Error("Get() returned product with no variants")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.Get(ctx, "nonexistent")
		if err != ErrProductNotFound {
			t.Errorf("Get() error = %v, want %v", err, ErrProductNotFound)
		}
	})
}

func (c *RepositoryContract) testList(t *testing.T) {
	ctx := context.Background()
	repo := c.NewRepo(t)

	t.Run("default limit", func(t *testing.T) {
		products, _, err := repo.List(ctx, ListOptions{})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(products) == 0 {
			t.Error("List() returned empty list")
		}
		if len(products) > DefaultLimit {
			t.Errorf("List() returned %d products, want <= %d", len(products), DefaultLimit)
		}
	})

	t.Run("custom limit", func(t *testing.T) {
		products, _, err := repo.List(ctx, ListOptions{Limit: 5})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(products) > 5 {
			t.Errorf("List() returned %d products, want <= 5", len(products))
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		_, _, err := repo.List(ctx, ListOptions{Limit: MaxLimit + 1})
		if err != ErrInvalidLimit {
			t.Errorf("List() error = %v, want %v", err, ErrInvalidLimit)
		}
	})
}

func (c *RepositoryContract) testPagination(t *testing.T) {
	ctx := context.Background()
	repo := c.NewRepo(t)

	firstPage, cursor, err := repo.List(ctx, ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(firstPage) == 0 {
		t.Fatal("First page is empty")
	}

	if cursor == "" {
		t.Skip("Pagination not needed for small datasets")
	}

	secondPage, _, err := repo.List(ctx, ListOptions{Limit: 1, Cursor: cursor})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	seen := make(map[string]bool)
	for _, p := range firstPage {
		seen[p.ID] = true
	}
	for _, p := range secondPage {
		if seen[p.ID] {
			t.Errorf("Product %s appears in both pages", p.ID)
		}
	}
}
