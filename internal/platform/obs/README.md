# Observability Package

This package provides metrics, distributed tracing, and log hygiene for the kikapu platform.

## Metrics

### HTTP Request Metrics

**kikapu_http_requests_total**
- Type: Counter
- Labels:
  - `route`: The HTTP route pattern (e.g., `/v1/products/{slug}`, `/v1/carts/{id}`)
  - `method`: HTTP method (GET, POST, PUT, DELETE, etc.)
  - `status`: HTTP status code

**kikapu_http_request_duration_seconds**
- Type: Histogram
- Labels:
  - `route`: The HTTP route pattern
- Buckets: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s

### Checkout Metrics

**kikapu_checkout_total**
- Type: Counter
- Labels:
  - `result`: Checkout result (success, declined, stock_conflict, error)

## Log Schema

All logs include the following required fields:
- `request_id`: Unique request identifier
- `route`: HTTP route pattern (normalized)
- `method`: HTTP method
- `status`: HTTP status code
- `duration_ms`: Request duration in milliseconds
- `error_code`: Error code if applicable

### Log Fields Never Logged

The following fields are explicitly **not** logged to prevent PII leakage:
- Request body
- Request headers
- Authentication tokens
- Phone numbers
- Credit card numbers (PAN)

### Phone Number Redaction

Phone numbers in Kenya (+254 country code) are automatically redacted:
- Format: `+2547*****678` or `2547****678`
- Recognizes space-separated, dash-separated, and raw formats

Example:
```
Input: "254712345678"
Output: "2547****678"
```

## Distributed Tracing

The service supports OpenTelemetry (OTLP) distributed tracing through the `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable.

- If `OTEL_EXPORTER_OTLP_ENDPOINT` is set, traces are exported to the configured endpoint
- If not set, tracing is disabled (no-op tracer)

### Setting up OTLP

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="localhost:4317"
./kikapu
```

## Adding Shopify Orders Log Field

When logging Shopify orders, the log output will include the additional field:

```log
{
  "level": "info",
  "request_id": "abc-123",
  "route": "/v1/checkout",
  "method": "POST",
  "status": 200,
  "duration_ms": 123.456,
  "shopify_order_id": 123456789,
  "message": "checkout successful"
}
```

## Usage Example

```go
package obs

// Setup metrics server
metricsHandler := NewMetricsHandler(metrics)
metricsServer := &http.Server{
    Addr: ":9090",
    Handler: metricsHandler,
}

// Middleware for HTTP requests
middleware := NewMiddleware(obs.MiddlewareOptions{
    Logger: logger,
    RequestID: get request ID context,
})

// Add Shopify orders log field
log.Info("checkout successful", "shopify_order_id", orderID)
```

## Testing

Run the tests:

```bash
go test ./internal/platform/obs/... -v
```

The test coverage includes:
- Metrics registry increments on request
- Route label is the pattern, not the raw path
- Log redaction patterns (phone numbers, PAN)
- Full checkout emits `checkout_total{result="success"}` once
