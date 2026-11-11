package obs

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsMiddlewareIncrementsCounter(t *testing.T) {
	oldRequestsTotal := RequestsTotal
	oldRequestDuration := RequestDuration

	defer func() {
		RequestsTotal = oldRequestsTotal
		RequestDuration = oldRequestDuration
	}()

	registry := prometheus.NewRegistry()
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kikapu_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"route", "method", "status"},
	)
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kikapu_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"route"},
	)
	registry.MustRegister(RequestsTotal, RequestDuration)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRoutePattern(r, "/v1/products/{slug}")
		w.WriteHeader(http.StatusOK)
	})

	middleware := MetricsMiddleware(handler)

	req := httptest.NewRequest("GET", "/v1/products/test-product", nil)
	rec := httptest.NewRecorder()

	beforeCount := testutil.ToFloat64(RequestsTotal.WithLabelValues("/v1/products/{slug}", "GET", "200"))

	middleware.ServeHTTP(rec, req)

	afterCount := testutil.ToFloat64(RequestsTotal.WithLabelValues("/v1/products/{slug}", "GET", "200"))

	if afterCount-beforeCount != 1 {
		t.Errorf("Expected counter to increment by 1, got %f", afterCount-beforeCount)
	}
}

func TestRoutePatternExtraction(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		pattern       string
		expectedRoute string
	}{
		{
			name:          "product endpoint",
			path:          "/v1/products/abc",
			pattern:       "/v1/products/{slug}",
			expectedRoute: "/v1/products/{slug}",
		},
		{
			name:          "cart endpoint",
			path:          "/v1/carts/123",
			pattern:       "GET /v1/carts/{id}",
			expectedRoute: "GET /v1/carts/{id}",
		},
		{
			name:          "product list",
			path:          "/v1/products",
			pattern:       "/v1/products",
			expectedRoute: "/v1/products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			SetRoutePattern(req, tt.pattern)

			route := GetRoutePattern(req)
			if route != tt.expectedRoute {
				t.Errorf("Expected route %q, got %q", tt.expectedRoute, route)
			}
		})
	}
}

func TestRoutePatternFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/products/abc", nil)

	route := GetRoutePattern(req)

	if route != "/v1/products/abc" {
		t.Errorf("Expected fallback route %q, got %q", "/v1/products/abc", route)
	}
}

func TestInstrumentCheckout(t *testing.T) {
	oldCheckoutTotal := CheckoutTotal
	defer func() {
		CheckoutTotal = oldCheckoutTotal
	}()

	registry := prometheus.NewRegistry()
	CheckoutTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kikapu_checkout_total",
			Help: "Total number of checkout operations",
		},
		[]string{"result"},
	)
	registry.MustRegister(CheckoutTotal)

	beforeSuccess := testutil.ToFloat64(CheckoutTotal.WithLabelValues(CheckoutResultSuccess))
	beforeError := testutil.ToFloat64(CheckoutTotal.WithLabelValues(CheckoutResultError))

	InstrumentCheckout(CheckoutResultSuccess)
	InstrumentCheckout(CheckoutResultError)

	afterSuccess := testutil.ToFloat64(CheckoutTotal.WithLabelValues(CheckoutResultSuccess))
	afterError := testutil.ToFloat64(CheckoutTotal.WithLabelValues(CheckoutResultError))

	if afterSuccess-beforeSuccess != 1 {
		t.Errorf("Expected success counter to increment by 1, got %f", afterSuccess-beforeSuccess)
	}
	if afterError-beforeError != 1 {
		t.Errorf("Expected error counter to increment by 1, got %f", afterError-beforeError)
	}
}

func TestMetricsMiddlewareDifferentStatus(t *testing.T) {
	oldRequestsTotal := RequestsTotal
	oldRequestDuration := RequestDuration

	defer func() {
		RequestsTotal = oldRequestsTotal
		RequestDuration = oldRequestDuration
	}()

	registry := prometheus.NewRegistry()
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kikapu_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"route", "method", "status"},
	)
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kikapu_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"route"},
	)
	registry.MustRegister(RequestsTotal, RequestDuration)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRoutePattern(r, "/v1/products/{slug}")
		w.WriteHeader(http.StatusNotFound)
	})

	middleware := MetricsMiddleware(handler)

	req := httptest.NewRequest("GET", "/v1/products/nonexistent", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	count := testutil.ToFloat64(RequestsTotal.WithLabelValues("/v1/products/{slug}", "GET", strconv.Itoa(http.StatusNotFound)))
	if count != 1 {
		t.Errorf("Expected counter to be 1, got %f", count)
	}
}
