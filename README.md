# kikapu

A headless commerce engine in Go - single binary, Postgres backend, REST API with OpenAPI spec.

## Overview

Kikapu is designed as a lightweight, single-binary commerce engine that can run in API or worker mode. It uses PostgreSQL for persistence and exposes a RESTful API.

## Features

- Single binary deployment
- PostgreSQL backend
- REST API with JSON responses
- Structured logging (JSON)
- Request tracing with request IDs
- Graceful shutdown support
- Health and readiness endpoints

## Project Structure

```
.
├── cmd/
│   └── kikapu/
│       └── main.go          # Application entry point
├── internal/
│   └── platform/
│       ├── config/         # Configuration management
│       ├── httpx/          # HTTP utilities (JSON, error envelope)
│       ├── money/          # Money handling (minor units)
│       └── obs/            # Observability (logging)
├── go.mod
├── Makefile
└── README.md
```

## Quick Start

### Prerequisites

- Go 1.27+
- PostgreSQL 14+

### Build

```bash
make build
```

### Run

```bash
./kikapu --addr :8080 --mode api
```

Or use the Makefile:

```bash
make run
```

### Test

```bash
make test
```

## Configuration

Configuration is environment-based:

| Variable | Default | Description |
|----------|---------|-------------|
| `KIKAPU_ADDR` | `:8080` | Address to listen on |
| `KIKAPU_MODE` | `api` | Operation mode (api\|worker) |
| `KIKAPU_LOG_LEVEL` | `info` | Log level (debug\|info\|warn\|error) |

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Health check - returns `{"status":"ok"}` |
| GET | `/readyz` | Readiness check - returns 200 |
| GET | `/v1/version` | Version info - returns version and commit |
| GET | `/v1/products` | List products with pagination |
| GET | `/v1/products/{slug}` | Get a single product by slug |

## Products API Examples

### List Products

List products with default pagination (20 items per page):

```bash
curl http://localhost:8080/v1/products
```

Response:
```json
{
  "data": [
    {
      "id": "prod001",
      "slug": "wireless-mouse",
      "name": "Wireless Mouse",
      "status": "published",
      "variants": [
        {"id": "var001", "sku": "WM-BLK-001", "price": {"value": 2999, "currency": "USD"}, "currency": "USD"},
        {"id": "var002", "sku": "WM-WHT-001", "price": {"value": 3499, "currency": "USD"}, "currency": "USD"}
      ]
    }
  ],
  "next_cursor": "eyJyYW5kb20I6Imxhc3RfaWQifQ=="
}
```

### Pagination

Use `limit` parameter (default: 20, max: 100):

```bash
curl "http://localhost:8080/v1/products?limit=10"
```

Use `cursor` parameter for pagination:

```bash
curl "http://localhost:8080/v1/products?limit=10&cursor=eyJyYW5kb20I6Imxhc3RfaWQifQ=="
```

### Sparse Fieldsets

Request specific fields only:

```bash
curl "http://localhost:8080/v1/products?fields=id,name"
```

Request variant fields:

```bash
curl "http://localhost:8080/v1/products?fields=id,variants.sku"
```

### Get a Single Product

```bash
curl http://localhost:8080/v1/products/wireless-mouse
```

Response:
```json
{
  "id": "prod001",
  "slug": "wireless-mouse",
  "name": "Wireless Mouse",
  "status": "published",
  "variants": [
    {"id": "var001", "sku": "WM-BLK-001", "price": {"value": 2999, "currency": "USD"}, "currency": "USD"}
  ]
}
```

### Error Responses

All errors follow the standard error envelope format:

```json
{
  "error": {
    "code": "product_not_found",
    "message": "product not found"
  }
}
```

## Development

### Code Quality

```bash
make vet      # Run go vet
make fmt      # Format code
make test     # Run tests
```

### CI

CI runs automatically on push:
- `gofmt -l .` must be clean
- `go vet ./...` must pass
- `go build ./...` must succeed
- `go test ./...` must pass

## Architecture Invariants

- Single binary deployment
- No in-process state as source of truth (state lives in Postgres)
- Money represented in minor units (cents, pence, etc.)
- All mutations are idempotent where possible
- Small, focused commits

## License

MIT
