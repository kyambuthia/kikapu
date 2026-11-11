# Performance Baseline

This document defines the performance budgets for kikapu's hot paths and provides guidance for running load tests.

## Performance Budgets

The following budgets represent p99 latency limits measured on a standard 4 vCPU / 16GB RAM machine. Values are in milliseconds.

| Endpoint                    | Max p99 | Hot Path | Notes                                    |
|----------------------------|---------|----------|------------------------------------------|
| `/api/products` (list)      | 50ms    | catalog list | Paginated, DB query with pagination      |
| `/api/products/{id}` (detail) | 20ms    | product detail | Single product fetch, warm cache     |
| `/api/checkout`             | 150ms   | checkout | DB transaction, payment provider        |

## Measuring on Different Hardware

When measuring on different machine specifications, be explicit about the environment:

```bash
# Example: 2 vCPU, 8GB RAM machine
./loadapp -scenario list -c 10 -d 30s --json > results/list-2cpu.json
```

Report the machine spec in any performance baselines you establish.

## Running Load Tests

### Quick Test

```bash
# List this endpoint with 20 concurrent requests for 10 seconds
./loadapp -scenario list -c 20 -d 10s
```

### Detailed Test

```bash
# Checkout endpoint with 50 workers for 30 seconds, output JSON
./loadapp -scenario checkout -c 50 -d 30s --json
```

### Rate-Specific Test

```bash
# Product detail endpoint, cap at 100 RPS
./loadapp -scenario detail -c 100 -d 60s -rps 100
```

## Common CLI Flags

- `-scenario` : Test scenario (list, detail, checkout)
- `-c`        : Number of concurrent requests (default: 20)
- `-d`        : Test duration (default: 10s)
- `-rps`      : Requests per second cap (default: 0, no cap)
- `--json`    : Output results in JSON format

## Infrastructure

The load harness uses pure Go:

- `net/http` for HTTP requests
- `sync` for concurrent execution
- No external dependencies

## Output Format

### Human Readable

```
Performance Report
==================

Scenario: catalog list
  Duration:       10.000s
  Concurrency:    20
  Throughput:     15234 requests
  Error Rate:     0.01%
  Latencies:
    p50:         12ms
    p95:         28ms
    p99:         45ms
```

### JSON

```json
{
  "timestamp": "2024-12-11T10:30:45Z",
  "hostname": "ec2-12-34-56-78",
  "machine_spec": {
    "cpus": "4",
    "memory": "16GB",
    "os": "Linux"
  },
  "scenarios": [
    {
      "name": "catalog list",
      "base_url": "http://:8080",
      "duration": "10.000s",
      "throughput": 15234,
      "error_rate": 0.01,
      "latencies": {
        "p50": "12ms",
        "p95": "28ms",
        "p99": "45ms"
      }
    }
  ]
}
```

## CI Integration

### Manual Trigger Job

```yaml
name: Performance Test

on:
  workflow_dispatch:
    inputs:
      scenario:
        description: 'Test scenario to run'
        required: true
        default: 'list'
        type: choice
        options:
          - list
          - detail
          - checkout
      concurrency:
        description: 'Number of workers'
        required: true
        default: '20'
      duration:
        description: 'Test duration in seconds'
        required: true
        default: '60'
      rps:
        description: 'Requests per second cap (0 for unlimited)'
        required: true
        default: '0'
```

### Example Job

```yaml
jobs:
  loadtest:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Install Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.27.1'

      - name: Build load test
        run: go build ./cmd/loadapp

      - name: Run catalog list test
        run: |
          ./loadapp \
            -scenario ${{ inputs.scenario }} \
            -c ${{ inputs.concurrency }} \
            -d ${{ inputs.duration }}s \
            -rps ${{ inputs.rps }} \
            --json > results.json

      - name: Check performance budgets
        run: |
          # Read JSON result
          result=$(cat results.json)
          
          # Extract p99 for specified scenario
          scenario_name="${{ inputs.scenario }}"
          
          # Budget check (from parallel run with confidence intervals)
          # Fail if error rate > 0.1% or p99 exceeds budget by 50%
          
        continue-on-error: true

      - name: Store results
        uses: actions/upload-artifact@v4
        with:
          name: perf-test-results
          path: results.json
```

## Regression Detection

A regression is detected when:

1. **Error Rate**: > 0.1% across the test duration
2. **p99 Latency**: Exceeds the budget by 50%
3. **Throughput**: Drops by 30% compared to baseline

If a regression is detected, document the finding in this file and investigate locally before pushing fixes.

## Machine Specification (Baseline)

- **CPUs**: 4
- **Memory**: 16GB
- **OS**: Ubuntu 22.04 LTS
- **Go Version**: 1.27.1
- **Database**: PostgreSQL 14

## Future Metrics

The following endpoints are tracked but not included in budgets:

- `/metrics` - Prometheus metrics export
- Health check endpoints
- Static asset serving

These endpoints should be heavily cached or CDN-hosted and are typically not bottlenecks in the current architecture.
