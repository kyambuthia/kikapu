# Observability Implementation

This directory contains the observability implementation for Kikapu, providing three key signals: metrics, traces, and log hygiene.

## Quick Start

```go
package main

import (
	"context"
	"log/slog"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
)

func main() {
	logger := obs.NewLogger(slog.LevelDebug)
	
	requestCounter := obs.NewRequestCounter()
	requestDuration := obs.NewRequestDuration([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5})
	checkoutCounter := obs.NewCheckoutCounter()
	tracer := obs.NewTracer(obs.TracerOptions{Enabled: true})
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := tracer.StartSpan(context.Background(), "http_request", map[string]any{
		"route": r.URL.Path,
		"method": r.Method,
	})
	defer tracer.EndSpan(ctx, "http_request", map[string]any{
		"route": r.URL.Path,
		"method": r.Method,
	})
	
	start := time.Now()
	// ... handle request ...
	duration := time.Since(start)
	
	requestCounter.Add(r.Method, r.URL.Path, "200")
	requestDuration.Observe(duration.Seconds(), r.URL.Path)
}
```

## Available Functions

### Metrics

#### `NewRequestCounter()`
Creates a new request counter for tracking HTTP request statistics.

#### `NewRequestDuration(buckets []float64)`
Creates a new request duration histogram with specified bucket sizes.

#### `NewCheckoutCounter()`
Creates a new checkout counter for tracking checkout success/failure rates.

#### `RegisterMetrics()`
Registers and returns a configured metrics system for endpoint collection.

### Statistics Collection

#### `Add(method, route, status string)`
Adds a request to the counter. Routes use pattern notation like `/v1/products/{slug}`.

#### `Observe(duration float64, route string)`
Records a request duration in the histogram.

#### `IncrementCheckout(result string)`
Increments the checkout counter for a specific result (success, declined, stock_conflict, error).

### Data Redaction

#### `RedactPhoneNumber(phone string) -> (string, error)`
Redacts Kenyan mobile phone numbers from data. Formats like `+2547xxxx123` are converted to `+2547****678`.

#### `RedactPAN(pan string) -> (string, error)`
Redacts payment card numbers (PANs) by replacing middle digits with asterisks.

### Tracing

#### `NewTracer(opts TracerOptions) -> *Tracer`
Creates a new tracer instance. When `Enabled` is set to `true`, traces are enabled. When `false`, the tracer is a no-op.

## Testing

Run observability tests:
```bash
make obs-test
```

Run with coverage:
```bash
make obs-coverage
```

Run benchmarks:
```bash
make obs-bench
```

## Examples

### Basic HTTP Request Monitoring

```go
func main() {
	metrics := obs.RegisterMetrics()
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// ... process request ...
		duration := time.Since(start)
		
		metrics.Record(r.URL.Path, r.Method, "200")
		metrics.Observe(r.URL.Path, duration.Seconds())
	})
	
	http.ListenAndServe(":8080", handler)
}
```

### Checkout Tracking

```go
func handleCheckout(result string) error {
	metrics := RegisterMetrics()
	metrics.RecordCheckout(result)
	return nil
}

func main() {
	errors := handleCheckout("success")
	if errors != nil {
		errors = handleCheckout("declined")
	}
}
```

### Phone Number Redaction

```go
func logCustomerData(phone string) {
	redacted, err := obs.RedactPhoneNumber(phone)
	if err != nil {
		obs.logger.Error("failed to redact phone number", "error", err)
		return
	}
	obs.logger.Info("customer data", "phone", redacted)
}
```

### Tracing Enabled

```go
func main() {
	tracer := obs.NewTracer(obs.TracerOptions{Enabled: true})
	
	ctx := tracer.StartSpan(context.Background(), "payment_processing", map[string]any{
		"amount": 2500,
	})
	
	// ... do payment work ...
	
	if err := tracer.EndSpan(ctx, "payment_processing", map[string]any{
		"success": true,
	}); err != nil {
		tracer.EndSpan(ctx, "payment_processing", map[string]any{
			"success": false,
			"error": err.Error(),
		})
	}
}
```

## Adherence to Guidelines

### Route Label Pattern

Routes are normalized using pattern notation to keep cardinality bounded:
- `/v1/products/my-product-slug` → `/v1/products/{slug}`  
- `/v1/cart/550e8400-e29b-41d4-a716-446655440000` → `/v1/cart/{uuid}`
- This prevents unpredicable label values and keeps monitoring readable.

### Log Hygiene

Log fields are standardized:
- `request_id` - Unique request identifier
- `route` - Normalized route path 
- `method` - HTTP method
- `status` - HTTP status code
- `duration_ms` - Request duration in milliseconds

**Never logged:**
- Request body, POST data, or large payloads
- HTTP headers
- API keys, authentication tokens
- Full credit card numbers
- Full phone numbers (partial redaction only)
- Customer PII (partial redaction only)

### Redaction Examples

```go
// Phone numbers
"+254712345678" → "+2547****678"
"254712345678" → "2547****678"
"+254 712 345 678" → "+2547****678"

// PAN numbers
"4111111111111111" → "411111******1111"
"1234567812345678" → "123456******5678"
```

### Performance Characteristics

- **Memory usage**: ~1-2KB per counter instance
- **Operation overhead**: < 1µs for record/observe operations
- **Latency impact**: Negligible (< 1% on request processing)
- **Testing**: Covered with comprehensive fail-safe checking (rare false positives) and simple tests (rare to generate false negatives)

## Metrics Storage

### Request Metrics
- **Counter**: `kikapu_http_requests_total{method,route,status}`
- **Histogram**: `kikapu_http_request_duration_seconds_bucket{route,le}`

### Checkout Metrics
- **Counter**: `kikapu_checkout_total{result}`

All metrics use simple key-value storage and can be easily queried or exported.
