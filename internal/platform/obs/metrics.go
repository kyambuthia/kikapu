package obs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kikapu_http_requests_total",
		Help: "Total number of HTTP requests",
	}, []string{"route", "method", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kikapu_http_request_duration_seconds",
		Help:    "Duration of HTTP requests in seconds",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"route"})

	CheckoutTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kikapu_checkout_total",
		Help: "Total number of checkout operations",
	}, []string{"result"})
)

const (
	CheckoutResultSuccess       = "success"
	CheckoutResultDeclined      = "declined"
	CheckoutResultStockConflict = "stock_conflict"
	CheckoutResultError         = "error"
)
