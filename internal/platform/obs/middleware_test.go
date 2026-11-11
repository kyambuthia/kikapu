package obs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestMetricsMiddleware_IncrementsCounters(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/v1/products/abc", nil)
	req = SetRoutePattern(req, "/v1/products/{id}")

	rec := httptest.NewRecorder()

	MetricsMiddleware(handler).ServeHTTP(rec, req)

	metric := &dto.Metric{}
	err := RequestsTotal.WithLabelValues("/v1/products/{id}", "GET", "200").Write(metric)
	if err != nil {
		t.Errorf("Failed to get metric: %v", err)
	}
	if metric.GetCounter().GetValue() != 1 {
		t.Errorf("Expected counter value 1, got %v", metric.GetCounter().GetValue())
	}
}

func TestSetAndGetRoutePattern(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/products/abc", nil)
	pattern := SetRoutePattern(req, "/v1/products/{id}")

	if GetRoutePattern(pattern) != "/v1/products/{id}" {
		t.Errorf("Expected pattern /v1/products/{id}, got %v", GetRoutePattern(pattern))
	}
}

func TestGetRoutePatternFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/products/abc", nil)
	if GetRoutePattern(req) != "/v1/products/abc" {
		t.Errorf("Expected fallback to path, got %v", GetRoutePattern(req))
	}
}

func TestRoutePattern_Extraction(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "uuid parameter",
			path: "/v1/products/550e8400-e29b-41d4-a716-446655440000",
			want: "/v1/products/{id}",
		},
		{
			name: "numeric parameter",
			path: "/v1/orders/12345",
			want: "/v1/orders/{id}",
		},
		{
			name: "slug parameter",
			path: "/v1/products/wireless-mouse",
			want: "/v1/products/{id}",
		},
		{
			name: "static path",
			path: "/v1/products",
			want: "/v1/products",
		},
		{
			name: "root path",
			path: "/",
			want: "/",
		},
		{
			name: "multi segment",
			path: "/v1/carts/550e8400-e29b-41d4-a716-446655440000/items",
			want: "/v1/carts/{id}/items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractRoutePattern(tt.path); got != tt.want {
				t.Errorf("ExtractRoutePattern(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"550e8400-e29b-41d4-a716-44665544000", false},
		{"not-a-uuid", false},
		{"", false},
		{"550e8400-e29b-41d4-a716-446655440000-extra", false},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := isUUID(tt.s); got != tt.want {
				t.Errorf("isUUID(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"12345", true},
		{"abc123", false},
		{"", false},
		{"123abc", false},
		{"0", true},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := isNumeric(tt.s); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestIsSlug(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"wireless-mouse", true},
		{"abc", false},
		{"123", false},
		{"ab", false},
		{"product-name-123", true},
		{"product_name", false},
		{"", false},
		{"a", false},
		{"ab!", false},
		{"productname", false},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := isSlug(tt.s); got != tt.want {
				t.Errorf("isSlug(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}
