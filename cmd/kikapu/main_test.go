package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/admin"
	"github.com/kyambuthia/kikapu/internal/auth"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/channel"
	"github.com/kyambuthia/kikapu/internal/checkout"
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

	mux := buildAPIMux(apiDeps{
		catalogRepo:     repo,
		regionRepo:      regionRepo,
		channelRepo:     channelRepo,
		cartStore:       cartStore,
		checkoutSvc:     checkoutSvc,
		adminProducts:   admin.NewMemoryProductRepository(),
		adminStock:      admin.NewMemoryStockRepository(),
		adminOrderStore: admin.NewMemoryOrderLister(),
		authStore:       auth.NewInMemoryStore(),
	})

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", want: http.StatusOK},
		{name: "readyz", method: http.MethodGet, path: "/readyz", want: http.StatusOK},
		{name: "version", method: http.MethodGet, path: "/v1/version", want: http.StatusOK},
		{name: "metrics", method: http.MethodGet, path: "/metrics", want: http.StatusOK},
		{name: "regions", method: http.MethodGet, path: "/v1/regions", want: http.StatusOK},
		{name: "channels", method: http.MethodGet, path: "/v1/channels", want: http.StatusOK},
		{name: "products", method: http.MethodGet, path: "/v1/products", want: http.StatusOK},
		{name: "admin products", method: http.MethodGet, path: "/v1/admin/products", want: http.StatusOK},
		{name: "admin orders", method: http.MethodGet, path: "/v1/admin/orders", want: http.StatusOK},
		{name: "auth keys", method: http.MethodGet, path: "/v1/auth/keys", want: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s %s status = %d, want %d (body: %s)", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
