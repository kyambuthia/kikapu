# Performance Testing Guide

## Overview

This document defines performance budgets and testing procedures for the kikapu application. The goal is to establish repeatable load tests that provide documented baselines for the three hot paths.

## Performance Budgets

The following budgets apply to the three primary hot paths. These budgets were measured on a 4 vCPU server with the following specifications:

- **CPU**: 4 vCPUs
- **Memory**: 8 GB
- **OS**: Linux 6.x
- **Go Version**: 1.27.1
- **Database**: PostgreSQL (in-memory for testing)

### List Products (Paginated)

- **Budget**: P99 latency < 50 ms
- **Endpoint**: `GET /v1/products`
- **Typical Response**: 20 products with variants
- **Key Bottlenecks**: N+1 query problem, in-memory filtering, cursor-based pagination overhead

### Product Detail

- **Budget**: P99 latency < 20 ms
- **Endpoint**: `GET /v1/products/{slug}`
- **Typical Response**: Single product with variants
- **Key Bottlenecks**: Left JOIN complexity, sparse fieldset parsing

### Checkout (Fake Payment)

- **Budget**: P99 latency < 150 ms
- **Endpoint**: `POST /v1/checkout`
- **Typical Response**: Order confirmation with paid status
- **Key Bottlenecks**: Multiple store operations, inventory locking, idempotency hashing

## Running Load Tests

### Prerequisites

1. Ensure the kikapu service is running:
   ```bash
   go run ./cmd/kikapu --addr :8080 --mode api
   ```

2. Verify the service is responding:
   ```bash
   curl http://localhost:8080/health
   ```

### Basic Usage

Run a single scenario with default settings (10 workers, 10 seconds):

```bash
go run ./loadtest -scenario list -c 20 -d 10s
```

### Available Scenarios

- `list`: Catalog list (paginated products)
- `detail`: Product detail view
- `checkout`: Checkout flow with fake payment

### Common Options

```bash
- -scenario <name>: Scenario to run (list, detail, checkout)
- -c <int>: Number of concurrent workers (default: 10, recommended: 20)
- -d <duration>: Duration of test (default: 10s, recommended: 60s)
- -rps <int>: Requests per second cap (0 = unlimited)
```

### Example Runs

#### Quick Test (List)

```bash
go run ./loadtest -scenario list -c 10 -d 5s
```

#### Stress Test (List)

```bash
go run ./loadtest -scenario list -c 50 -d 60s
```

#### Product Detail Benchmark

```bash
go run ./loadtest -scenario detail -c 20 -d 30s
```

#### Checkout Performance

```bash
go run ./loadtest -scenario checkout -c 5 -d 30s
```

#### All Scenarios (Keep this script for scheduled tests)

```bash
for scenario in list detail checkout; do
    echo "Testing $scenario..."
    go run ./loadtest -scenario $scenario -c 20 -d 60s
    echo "---"
done
```

## How to Read Results

The load test outputs both human-readable tables and JSON for machine consumption.

### Human-Readable Output

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

### Output Values Explained

- **Requests**: Total number of requests completed
- **Success Rate**: Percentage of successful requests (0-100%)
- **Failures**: Number of failed requests
- **Throughput**: Average requests per second during test duration
- **Latency Percentiles**: P50 (median), P95 (95th percentile), P99 (99th percentile), Min, Max, Mean, Std Dev
  - Use **P99** to check against performance budgets (most important single metric)
  - Use **P50** for typical response time expectations
  - Use **Mean** to understand overall bottlenecks

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

## Performance Regression Detection

### Failure Criteria

A test is considered a performance regression if **either**:

1. **Error Rate**: > 0.1% (i.e., > 0.001) of requests fail
2. **Latency Budget**: P99 latency exceeds the budget by more than 50%

#### Error Rate Threshold

- **Acceptable**: Error rate ≤ 0.1%
- **Failed**: Error rate > 0.1%

#### Latency Budget Enforcement

Each hot path has a specified budget with a 50% margin:

| Hot Path | Budget | Warning Margin | Failed |
|----------|--------|----------------|--------|
| List     | < 50ms | < 75ms         | ≥ 75ms |
| Detail   | < 20ms | < 30ms         | ≥ 30ms |
| Checkout | <150ms | < 225ms        | ≥225ms |

#### Example Regression Determination

If a `list` test produces:

- P99 latency: 80ms
- Error rate: 0.05%

**Result**: Failed (P99 exceeds warning margin by 33%)

If a `detail` test produces:

- P99 latency: 25ms
- Error rate: 0.2%

**Result**: Failed (Error rate exceeds 0.1% threshold)

## Common Issues and Solutions

### High P99 Latency on List

**Possible Causes**:

1. N+1 query problem (each product gets a separate DB query for variants)
2. Large number of products or variants
3. Insufficient database capacity
4. In-memory filtering overhead

**Check**:
```bash
# Verify database performance
psql -c "EXPLAIN ANALYZE SELECT * FROM products LIMIT 20"
```

**Fixes**:
- Optimize the product list query to fetch all variants in a single JOIN
- Add database indexes on frequently queried columns
- Reduce the number of variants per product

### High P99 Latency on Detail

**Possible Causes**:

1. Left JOIN returning too many rows
2. Sparse fieldset validation overhead
3. Missing database indexes on product slug

**Check**:
```bash
# Check execution plan
psql -c "EXPLAIN ANALYZE SELECT p.*, v.* FROM products p LEFT JOIN variants v ON v.product_id = p.id WHERE p.slug = 'valid-product-slug'"
```

**Fixes**:
- Add composite index: `CREATE INDEX idx_products_slug ON products(slug)`
- Ensure variant data size is appropriate

### High Latency on Checkout

**Possible Causes**:

1. Inventory store locking contention
2. Multiple cart operations
3. Idempotency hash collisions
4. Fake payment processing overhead

**Check**:
```bash
# Check lock contention
# Monitor system metrics during checkout load test
top -b -n 1 | head -20
```

**Fixes**:
- Optimize inventory reservation logic
- Check for N+1 patterns in inventory operations
- Reduce number of cart line items per checkout

### Sudden Spike in Error Rate

**Possible Causes**:

1. Database connection pool exhaustion
2. Service crashes or panics
3. Network issues
4. Resource limits (file descriptors, memory)

**Check**:
```bash
# Check resource usage
dmesg | tail -20
free -h
ulimit -a
```

**Fixes**:
- Increase connection pool size
- Add retry logic with exponential backoff
- Monitor system resources and adjust limits

## CI Job

A GitHub Actions job can be added for automated performance testing. The job should:

1. Run each scenario for 60 seconds
2. Check error rate ≤ 0.1%
3. Check P99 latency within specified budgets
4. Upload results as artifacts
5. Patch the regression if it's a trivial fix and budget is exceeded by < 50%

### Example GitHub Actions Workflow

```yaml
name: Performance Test

on:
  workflow_dispatch:
    inputs:
      scenario:
        description: 'Scenario to test'
        required: true
        default: 'list'
        type: choice
        options:
          - list
          - detail
          - checkout
      concurrency:
        description: 'Concurrent workers'
        required: false
        default: '20'
      duration:
        description: 'Test duration'
        required: false
        default: '60s'

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - run: go build ./cmd/kikapu

      - name: Load test
        run: |
          # Start service in background
          kikapu --addr :8080 > /tmp/kikapu.log 2>&1 &
          SERVICE_PID=$!
          sleep 5

          # Run load test
          go run ./loadtest \
            -scenario ${{ inputs.scenario }} \
            -c ${{ inputs.concurrency }} \
            -d ${{ inputs.duration }}

          # Stop service
          kill $SERVICE_PID
          wait $SERVICE_PID 2>/dev/null || true

      - name: Upload results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: performance-results-${{ inputs.scenario }}
          path: loadtest_results.txt

      - name: Check performance
        run: |
          ERROR_RATE=$(grep "Error Rate:" loadtest_results.txt | awk '{print $3}' | tr -d '%')
          P99_LATENCY=$(grep "P99:" loadtest_results.txt | awk '{print $3}')

          echo "Error rate: $ERROR_RATE%"
          echo "P99 latency: $P99_LATENCY ms"

          if (( $(echo "$ERROR_RATE > 0.1" | bc -l) )); then
            echo "FAILURE: Error rate exceeds 0.1%"
            exit 1
          fi
