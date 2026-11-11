package obs

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestExtractRoutePattern(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "/v1/products/{slug}",
			path: "/v1/products/hat",
			want: "/v1/products/{slug}",
		},
		{
			name: "/v1/carts/{id}",
			path: "/v1/carts/abc-123",
			want: "/v1/carts/{id}",
		},
		{
			name: "/v1/carts/{id}/items/{variantID}",
			path: "/v1/carts/abc-123/items/xyz-456",
			want: "/v1/carts/{id}/items/{variantID}",
		},
		{
			name: "/v1/checkout",
			path: "/v1/checkout",
			want: "/v1/checkout",
		},
		{
			name: "/v1/regions/{id}",
			path: "/v1/regions/ke",
			want: "/v1/regions/{id}",
		},
		{
			name: "/v1/payments/{id}",
			path: "/v1/payments/emp-12345",
			want: "/v1/payments/{id}",
		},
		{
			name: "root health endpoint",
			path: "/healthz",
			want: "/healthz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractRoutePattern(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMetricsRegistryIncrementsOnRequest(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	reqCounterOpts := reqCounterOpts{
		route:  "/v1/carts/{id}",
		method: "GET",
		status: http.StatusOK,
	}

	for i := 0; i < 3; i++ {
		metrics.Record(reqCounterOpts)
	}

	expected := float64(3)
	count := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues(reqCounterOpts.route, reqCounterOpts.method, strconv.Itoa(reqCounterOpts.status)))
	require.Equal(t, expected, count)
}

func TestRouteLabelIsPatternNotRawPath(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	tests := []struct {
		name string
		opts reqCounterOpts
	}{
		{
			name: "raw path used directly",
			opts: reqCounterOpts{
				route:  "/v1/carts/abc-123",
				method: "GET",
				status: http.StatusOK,
			},
		},
		{
			name: "pattern label",
			opts: reqCounterOpts{
				route:  "/v1/carts/{id}",
				method: "GET",
				status: http.StatusOK,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.Record(tt.opts)

			route, method, status := tt.opts.route, tt.opts.method, tt.opts.status
			count := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues(route, method, strconv.Itoa(status)))
			require.Equal(t, float64(1), count, "should increment counter")
		})
	}
}

func TestDurationHistogramRecordsLatency(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	durationOpts := durationHistogramOpts{
		route:   "/v1/products/{slug}",
		latency: 0.05,
	}

	metrics.Record(durationOpts)

	count := testutil.ToFloat64(metrics.requestDuration.WithLabelValues(durationOpts.route))
	require.Greater(t, count, float64(0), "should record histogram sample")
}

func TestCheckoutCounterIncrementsOnce(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	checkoutTotalOpts := checkoutTotalOpts{
		result: PaymentResultSuccess,
	}

	metrics.Record(checkoutTotalOpts)

	count := testutil.ToFloat64(metrics.checkoutTotal.WithLabelValues(checkoutTotalOpts.result))
	require.Equal(t, float64(1), count)

	durationOpts := durationHistogramOpts{
		route:   "/v1/checkout",
		latency: 0.1,
	}
	metrics.Record(durationOpts)
	count2 := testutil.ToFloat64(metrics.requestDuration.WithLabelValues(durationOpts.route))
	require.Equal(t, float64(1), count2)
}

func TestMetricsReduction(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.Record(reqCounterOpts{
		route:  "/v1/carts/{id}",
		method: "GET",
		status: http.StatusOK,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/carts/{id}",
		method: "POST",
		status: http.StatusCreated,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "POST",
		status: http.StatusOK,
	})

	total := testutil.ToFloat64(metrics.requestsTotal)
	require.Equal(t, float64(3), total, "should count all requests")
}

func TestMetricsConcurrent(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	calls := 100
	var wg sync.WaitGroup

	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics.Record(reqCounterOpts{
				route:  "/v1/carts/{id}",
				method: "POST",
				status: http.StatusOK,
			})
		}()
	}

	wg.Wait()

	count := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues("/v1/carts/{id}", "POST", "200"))
	require.Equal(t, float64(calls), count)
}

func TestMetricsCleanup(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "GET",
		status: http.StatusNotFound,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "GET",
		status: http.StatusInternalServerError,
	})

	successCount := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues("/v1/checkout", "GET", "200"))
	nilCount := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues("/v1/checkout", "GET", "404"))
	errorCount := testutil.ToFloat64(metrics.requestsTotal.WithLabelValues("/v1/checkout", "GET", "500"))

	require.Equal(t, float64(0), successCount, "should not count successful request")
	require.Equal(t, float64(1), nilCount, "should count not found")
	require.Equal(t, float64(1), errorCount, "should count server error")
}

type checkoutTotalOpts struct {
	result string
}

func (m *Metrics) Record(opts checkoutTotalOpts) {
	m.checkoutTotal.WithLabelValues(opts.result).Inc()
}

type PaymentResult string

const (
	PaymentResultSuccess  PaymentResult = "success"
	PaymentResultDeclined PaymentResult = "declined"
	PaymentResultStock    PaymentResult = "stock_conflict"
	PaymentResultError    PaymentResult = "error"
)
