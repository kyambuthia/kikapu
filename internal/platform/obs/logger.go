package obs

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	otel_trace "go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

type Logger struct {
	logger *slog.Logger
}

func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		logger: l.logger.With(args...),
	}
}

func NewLogger(level slog.Level) *Logger {
	return &Logger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})),
	}
}

type Middleware struct {
	logger    *Logger
	requestID string
	metrics   *Metrics
	tracer    otel_trace.Tracer
	reqStart  time.Time
	method    string
	route     string
	status    int
	err       *string
	duration  atomic.Int64
}

type MiddlewareOptions struct {
	Logger    *Logger
	RequestID string
}

func NewMiddleware(opts MiddlewareOptions) *Middleware {
	return &Middleware{
		logger:    opts.Logger,
		requestID: opts.RequestID,
		metrics:   NewMetrics(),
		tracer:    GetTracer(),
	}
}

func (m *Middleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.reqStart = time.Now()
		m.method = r.Method

		route, err := extractRoutePattern(r.URL.Path)
		if err != nil {
			route = r.URL.Path
		}
		m.route = route

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		_, span := otel_trace.SpanFromContext(r.Context()).TracerProvider().Tracer("kikapu").Start(r.Context(), "http.request", otel_trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.Path),
		))
		defer span.End()

		next.ServeHTTP(rw, r)

		m.status = rw.statusCode

		duration := float64(time.Since(m.reqStart) / time.Millisecond)
		m.duration.Add(int64(duration))

		m.logger.Info("request",
			"request_id", m.requestID,
			"route", m.route,
			"method", m.method,
			"status", m.status,
			"duration_ms", duration,
		)

		m.metrics.Record(reqCounterOpts{
			route:  m.route,
			method: m.method,
			status: m.status,
		})

		m.metrics.Record(durationHistogramOpts{
			route:   m.route,
			latency: duration / 1000.0,
		})
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	err        *error
}

func (rw *responseWriter) Header() http.Header {
	return rw.ResponseWriter.Header()
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

var routePatternRegex = regexp.MustCompile(`^/v1/([^/]+)/([^/]+)`)

func extractRoutePattern(path string) (string, error) {
	matches := routePatternRegex.FindStringSubmatch(path)
	if len(matches) >= 3 {
		return fmt.Sprintf("/v1/%s/{id}", matches[2]), nil
	}

	if strings.HasPrefix(path, "/v1/carts/") {
		if strings.Contains(path, "/items/") {
			return "/v1/carts/{id}/items/{variantID}", nil
		}
		return "/v1/carts/{id}", nil
	}

	if strings.HasPrefix(path, "/v1/checkout") {
		return "/v1/checkout", nil
	}

	if strings.HasPrefix(path, "/v1/products/") {
		return "/v1/products/{slug}", nil
	}

	if strings.HasPrefix(path, "/v1/regions/") {
		return "/v1/regions/{id}", nil
	}

	if strings.HasPrefix(path, "/v1/payments/") {
		return "/v1/payments/{id}", nil
	}

	return path, nil
}

type Metrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	checkoutTotal   *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	return &Metrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kikapu_http_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"route", "method", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kikapu_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"route"}),
		checkoutTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kikapu_checkout_total",
			Help: "Total number of checkout attempts",
		}, []string{"result"}),
	}
}

func (m *Metrics) MustRegister() {
	prometheus.MustRegister(m.requestsTotal)
	prometheus.MustRegister(m.requestDuration)
	prometheus.MustRegister(m.checkoutTotal)
}

type reqCounterOpts struct {
	route  string
	method string
	status int
}

func (m *Metrics) Record(opts reqCounterOpts) {
	m.requestsTotal.WithLabelValues(opts.route, opts.method, strconv.Itoa(opts.status)).Inc()
}

type durationHistogramOpts struct {
	route   string
	latency float64
}

func (m *Metrics) Record(opts durationHistogramOpts) {
	m.requestDuration.WithLabelValues(opts.route).Observe(opts.latency)
}

type metricsHandler struct {
	metrics *Metrics
}

func NewMetricsHandler(metrics *Metrics) http.Handler {
	return &metricsHandler{metrics: metrics}
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.metrics.MustRegister()
	w.Header().Set("Content-Type", "text/plain")
	promhttp.Handler().ServeHTTP(w, r)
}

func initTracer() {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("kikapu"),
	)

	otlpConn, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(otlpConn),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
}

func init() {
	initTracer()
}
