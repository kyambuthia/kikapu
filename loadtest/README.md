# Load Testing

This directory contains a Go-based load test harness for the kikapu application.

## Overview

The load test harness is a standalone Go binary that tests the performance of three hot paths:

1. **Catalog List (Paginated)** - `GET /v1/products`
2. **Product Detail** - `GET /v1/products/{slug}`
3. **Checkout (Fake Payment)** - `POST /v1/checkout`

## Features

- No external dependencies (only `net/http` and `sync`)
- Configurable concurrency, duration, and RPS limits
- Statistical latency measurements (P50, P95, P99 percentiles)
- Success rate and error rate tracking
- JSON output for machine consumption
- Human-readable table output

## Building

```bash
cd loadtest
go build -o loadtest
```

## Running

### Basic Usage

```bash
# Run list scenario with default settings
go run ./loadtest -scenario list -c 20 -d 10s

# Run all scenarios
go run ./loadtest -scenario list -c 20 -d 60s
go run ./loadtest -scenario detail -c 20 -d 60s
go run ./loadtest -scenario checkout -c 5 -d 60s
```

### Options

```bash
-flag string
      Scenario to run (list, detail, checkout) (default "list")
-c int
      Number of concurrent workers (default 10)
-d duration
      Duration of the load test (default 10s)
```

### Example Scenarios

**Quick Test (5s)**:
```bash
go run ./loadtest -scenario list -c 10 -d 5s
```

**Stress Test (List)**:
```bash
go run ./loadtest -scenario list -c 50 -d 60s
```

**Checkout Performance**:
```bash
go run ./loadtest -scenario checkout -c 5 -d 30s
```

## Output Format

### Human-Readable

```
=== Load Test Results ===
Requests:     245
Success Rate: 100.00%
Failures:     0
Throughput:   24.50

=== Latency (ms) ===
P50:          12.34 ms
P95:          18.56 ms
P99:          45.67 ms
Min:          5.12 ms
Max:          89.23 ms
Mean:         15.67 ms
Std Dev:      8.45 ms
```

### JSON Output

```json
{
  "success_rate": 100.0,
  "throughput": 24.5,
  "latency_p50": 12340,
  "latency_p95": 18560,
  "latency_p99": 45670
}
```

## Integration with CI

See [`docs/PERFORMANCE.md`](../docs/PERFORMANCE.md) for:

- Performance budgets and verification rules
- Regression detection criteria
- Integration with GitHub Actions
- Troubleshooting common issues
