package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/platform/money"
	"github.com/kyambuthia/kikapu/internal/search"
)

func TestSearchHandler_InMemoryAndPublishedOnly(t *testing.T) {
	repo := catalog.NewInMemoryRepository([]catalog.Product{
		{
			ID:     "p1",
			Slug:   "wireless-mouse",
			Name:   "Wireless Mouse",
			Status: "published",
			Variants: []catalog.Variant{
				{ID: "v1", SKU: "WM-1", Price: money.NewAmount(2999, "USD"), Currency: "USD"},
			},
		},
		{
			ID:     "p2",
			Slug:   "draft-mouse",
			Name:   "Draft Mouse",
			Status: "draft",
			Variants: []catalog.Variant{
				{ID: "v2", SKU: "DM-1", Price: money.NewAmount(1999, "USD"), Currency: "USD"},
			},
		},
	})

	h := NewSearchHandler(search.NewInMemoryStore(repo))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Even if ?status=draft is passed, public search must only return published products.
	req := httptest.NewRequest(http.MethodGet, "/v1/search?q=mouse&status=draft", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var res search.Results
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].Slug != "wireless-mouse" {
		t.Fatalf("results = %+v, want only published wireless-mouse", res.Data)
	}
}
