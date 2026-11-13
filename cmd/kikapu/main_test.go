package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyambuthia/kikapu/internal/admin"
	"github.com/kyambuthia/kikapu/internal/auth"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/idempotency"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/region"
)

func TestBuildAPIMux_CoreEndpoints(t *testing.T) {
	repo := catalog.NewInMemoryRepository(nil)
	regionRepo := region.NewInMemoryRepository(defaultRegions())
	channelRepo := channel.NewInMemoryRepository(defaultChannels())
	cartStore := cart.NewInMemoryStore()
	invStore := inventory.NewMemoryStore(nil)
	checkoutSvc := checkout.NewService(
		cartStore,
		invStore,
		payment.NewFakeProvider(payment.NoFailure),
		checkout.NewIdempotencyStore(),
		outbox.NewInMemoryOutbox(),
	)

	authStore := auth.NewInMemoryStore()
	if _, err := authStore.Seed("test-admin", "kik_test_admin_key", []string{"admin"}); err != nil {
		t.Fatalf("seed admin key: %v", err)
	}
	if _, err := authStore.Seed("test-reader", "kik_test_reader_key", []string{"catalog:read"}); err != nil {
		t.Fatalf("seed reader key: %v", err)
	}

	mux := buildAPIMux(apiDeps{
		catalogRepo:     repo,
		regionRepo:      regionRepo,
		channelRepo:     channelRepo,
		cartStore:       cartStore,
		checkoutSvc:     checkoutSvc,
		adminProducts:   admin.NewMemoryProductRepository(),
		adminStock:      admin.NewMemoryStockRepository(),
		adminOrderStore: admin.NewMemoryOrderLister(),
		authStore:       authStore,
	})

	tests := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", want: http.StatusOK},
		{name: "readyz", method: http.MethodGet, path: "/readyz", want: http.StatusOK},
		{name: "version", method: http.MethodGet, path: "/v1/version", want: http.StatusOK},
		{name: "metrics", method: http.MethodGet, path: "/metrics", want: http.StatusOK},
		{name: "regions", method: http.MethodGet, path: "/v1/regions", want: http.StatusOK},
		{name: "channels", method: http.MethodGet, path: "/v1/channels", want: http.StatusOK},
		{name: "products", method: http.MethodGet, path: "/v1/products", want: http.StatusOK},
		{name: "admin products unauth", method: http.MethodGet, path: "/v1/admin/products", want: http.StatusUnauthorized},
		{name: "admin products wrong scope", method: http.MethodGet, path: "/v1/admin/products", token: "kik_test_reader_key", want: http.StatusForbidden},
		{name: "admin products auth", method: http.MethodGet, path: "/v1/admin/products", token: "kik_test_admin_key", want: http.StatusOK},
		{name: "admin orders auth", method: http.MethodGet, path: "/v1/admin/orders", token: "kik_test_admin_key", want: http.StatusOK},
		{name: "auth keys unauth", method: http.MethodGet, path: "/v1/auth/keys", want: http.StatusUnauthorized},
		{name: "auth keys auth", method: http.MethodGet, path: "/v1/auth/keys", token: "kik_test_admin_key", want: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s %s status = %d, want %d (body: %s)", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

type stubIdempotencyStore struct {
	records map[string]idempotency.Record
}

func (s *stubIdempotencyStore) Begin(_ context.Context, key, reqHash string) (idempotency.Record, bool, error) {
	if rec, ok := s.records[key]; ok {
		if rec.RequestHash != reqHash {
			return idempotency.Record{}, false, idempotency.ErrConflict
		}
		return rec, true, nil
	}
	s.records[key] = idempotency.Record{Key: key, RequestHash: reqHash}
	return s.records[key], false, nil
}

func (s *stubIdempotencyStore) Finish(_ context.Context, key string, status int, response []byte) error {
	rec := s.records[key]
	rec.Status = status
	rec.Response = append([]byte(nil), response...)
	s.records[key] = rec
	return nil
}

func (s *stubIdempotencyStore) Cleanup(_ context.Context) error {
	return nil
}

func TestOptionalIdempotencyMiddleware(t *testing.T) {
	store := &stubIdempotencyStore{records: make(map[string]idempotency.Record)}
	calls := 0
	handler := optionalIdempotencyMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	// POST without Idempotency-Key passes through without caching.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/carts", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}

	// POST with Idempotency-Key executes once and replays on retry.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/carts", strings.NewReader(`{"currency":"USD"}`))
		req.Header.Set("Idempotency-Key", "key-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("idempotent status = %d, want 201", rec.Code)
		}
	}
	if calls != 3 {
		t.Fatalf("calls after idempotent replay = %d, want 3", calls)
	}
}
