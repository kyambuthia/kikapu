package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandler_Orders(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	orderStore := NewMemoryOrderLister(
		Order{
			ID:                "ord-1",
			Number:            "ORD-0001",
			Status:            "confirmed",
			PaymentStatus:     "captured",
			FulfillmentStatus: "unfulfilled",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		Order{
			ID:                "ord-2",
			Number:            "ORD-0002",
			Status:            "pending",
			PaymentStatus:     "pending",
			FulfillmentStatus: "unfulfilled",
			CreatedAt:         now.Add(time.Minute),
			UpdatedAt:         now.Add(time.Minute),
		},
	)

	h := NewHandler(NewMemoryProductRepository(), NewMemoryStockRepository())
	h.SetOrderStore(orderStore)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("list all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/orders", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			Data       []Order `json:"data"`
			NextCursor string  `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("len(data) = %d, want 2", len(resp.Data))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/orders?status=confirmed", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			Data []Order `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Data) != 1 || resp.Data[0].ID != "ord-1" {
			t.Fatalf("unexpected filtered data: %+v", resp.Data)
		}
	})

	t.Run("get by id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/orders/ord-1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/orders/missing", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
