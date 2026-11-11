# kikapu

A headless commerce engine in Go - single binary, Postgres backend, REST API with OpenAPI spec.

## Overview

Kikapu is designed as a lightweight, single-binary commerce engine that can run in API or worker mode. It uses PostgreSQL for persistence and exposes a RESTful API.

**Documentation**: [Architecture](docs/ARCHITECTURE.md) | [Invariants](docs/INVARIANTS.md) | [Migrations](docs/MIGRATIONS.md) | [Roadmap](docs/ROADMAP.md) | [Standards](docs/STANDARDS.md) | [ADRs](docs/adr/)

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

### Database Setup

Kikapu requires PostgreSQL for persistence. Set up your database:

1. Create a PostgreSQL database:
```bash
createdb kikapu
```

2. Run migrations:
```bash
export DATABASE_URL="postgres://user:password@localhost:5432/kikapu?sslmode=disable"
./kikapu --mode migrate
```

Or specify migrations directory:
```bash
./kikapu --mode migrate --migrations ./migrations
```

### Run

```bash
export DATABASE_URL="postgres://user:password@localhost:5432/kikapu?sslmode=disable"
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

For integration tests with PostgreSQL:

```bash
export TEST_DATABASE_URL="postgres://user:password@localhost:5432/kikapu_test?sslmode=disable"
make test
```

Integration tests skip cleanly when `TEST_DATABASE_URL` is not set.

## Configuration

Configuration is environment-based:

| Variable | Default | Description |
|----------|---------|-------------|
| `KIKAPU_ADDR` | `:8080` | Address to listen on |
| `KIKAPU_MODE` | `api` | Operation mode (api\|worker\|migrate) |
| `KIKAPU_LOG_LEVEL` | `info` | Log level (debug\|info\|warn\|error) |
| `DATABASE_URL` | `` | PostgreSQL connection string (required for api/migrate modes) |
| `PAYMENT_PROVIDER` | `fake` | Payment provider (fake\|paystack\|mpesa) |
| `PAYSTACK_SECRET_KEY` | `` | Paystack secret key (required if PAYMENT_PROVIDER=paystack) |
| `PAYSTACK_BASE_URL` | `` | Paystack API base URL (optional, defaults to https://api.paystack.co) |
| `MPESA_CONSUMER_KEY` | `` | M-Pesa Daraja consumer key (required if PAYMENT_PROVIDER=mpesa) |
| `MPESA_CONSUMER_SECRET` | `` | M-Pesa Daraja consumer secret (required if PAYMENT_PROVIDER=mpesa) |
| `MPESA_SHORT_CODE` | `` | M-Pesa business short code (required if PAYMENT_PROVIDER=mpesa) |
| `MPESA_PASSKEY` | `` | M-Pesa passkey (required if PAYMENT_PROVIDER=mpesa) |
| `MPESA_CALLBACK_URL` | `` | M-Pesa callback URL (required if PAYMENT_PROVIDER=mpesa) |
| `MPESA_BASE_URL` | `` | M-Pesa API base URL (optional, defaults to https://sandbox.safaricom.co.ke) |
| `MPESA_ALLOWED_CIDRS` | `` | Comma-separated list of allowed CIDRs for M-Pesa callbacks (required for production) |
| `KIKAPU_RATE_LIMIT_CHECKOUT` | `10` | Rate limit for checkout endpoint (requests/min per key) |
| `KIKAPU_RATE_LIMIT_ADMIN` | `120` | Rate limit for admin endpoints (requests/min per key) |
| `KIKAPU_RATE_LIMIT_PUBLIC_READ` | `300` | Rate limit for public read endpoints (requests/min per IP) |
| `KIKAPU_RATE_LIMIT_MAX_BYTES` | `1048576` | Maximum request body size (1 MiB default) |
| `KIKAPU_RATE_LIMIT_TRUSTED_PROXIES` | `false` | Trust X-Forwarded-For header from proxies |

## Idempotency

All mutating endpoints support idempotency via the `Idempotency-Key` header. This ensures safe retries and prevents duplicate operations.

### Header

- **Name**: `Idempotency-Key`
- **Required**: Yes, for all POST, PATCH, PUT, DELETE endpoints
- **Format**: Any unique string (recommend UUID v4)
- **Example**: `Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000`

### Semantics

| Scenario | Behavior |
|----------|----------|
| First request with key | Executes normally, stores response |
| Same key + same request | Returns stored response (replay) |
| Same key + different request | 409 Conflict |
| Concurrent same-key requests | 409 Conflict with `Retry-After` |
| Key older than 24h | Treated as new request |

### Conflict Detection

The idempotency system uses a SHA-256 hash of the request (method + path + body) to detect conflicts:

- If the same key is reused with a **different** request, the system rejects it with `idempotency_conflict`
- This prevents accidental key reuse across different operations

### Expiration

- Idempotency keys expire after **24 hours**
- After expiration, the key can be reused for new operations
- A background cleanup worker removes expired records hourly

### Endpoints Requiring Idempotency

- `POST /v1/checkout` - Place order
- `POST /v1/orders/{id}/refund` - Refund order
- `POST /v1/admin/products` - Create product
- `PATCH /v1/admin/products/{id}` - Update product
- `DELETE /v1/admin/products/{id}` - Delete product
- `PUT /v1/admin/products/{id}/stock` - Update stock level

### Example: Retry Behavior

```bash
# First request - succeeds
curl -X POST http://localhost:8080/v1/checkout \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{"cart_id": "cart_001"}'

# Response: {"order_id": "ord_123", "status": "paid"}

# Retry with same key + same body - returns cached response
curl -X POST http://localhost:8080/v1/checkout \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{"cart_id": "cart_001"}'

# Response: {"order_id": "ord_123", "status": "paid"} (same order)

# Different cart with same key - conflict
curl -X POST http://localhost:8080/v1/checkout \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{"cart_id": "cart_002"}'

# Response: 409 Conflict
```

## Database Schema

Kikapu uses PostgreSQL with the following schema:

### Tables

- **products**: Product catalog (UUID primary keys, slug unique)
- **variants**: Product variants with price in minor units
- **product_channels**: Link table for multi-channel support
- **orders**: Order headers with region-scoped order numbers
- **order_lines**: Order line items
- **refunds**: Refund records

### Constraints

- **Money**: All monetary values stored as `BIGINT` (minor units) with `CHECK >= 0` where applicable
- **Order Numbers**: Unique per region with `UNIQUE (region_id, number)`
- **Timestamps**: `created_at` and `updated_at` with `DEFAULT now()`
- **No Cross-Module FKs**: Tables reference only within their module

### Migrations

Migrations are forward-only SQL files in `migrations/`. Each migration:

- Has a version prefix (e.g., `0001_init.sql`)
- Includes a comment header explaining the change
- Is applied idempotently with advisory locking

To create a new migration:

```bash
# Create migrations if directory doesn't exist
mkdir -p migrations

# Add your SQL migration file
cat > migrations/0002_add_new_table.sql <<EOF
-- Add new table for feature X
-- Why: Support feature X requirements
-- Reversible: no

CREATE TABLE new_table (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('0002');
EOF

# Run migration
./kikapu --mode migrate
```

## API Endpoints

 | Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Health check - returns `{"status":"ok"}` |
| GET | `/readyz` | Readiness check - returns 200 |
| GET | `/v1/version` | Version info - returns version and commit |
| GET | `/v1/products` | List products with pagination |
 | GET | `/v1/products/{slug}` | Get a single product by slug |
 | GET | `/v1/regions` | List all regions |
 | GET | `/v1/channels` | List all sales channels |
 | POST | `/v1/carts` | Create a new cart |
 | GET | `/v1/carts/{id}` | Get a cart by ID |
 | DELETE | `/v1/carts/{id}` | Delete (clear) a cart |
 | POST | `/v1/carts/{id}/items` | Add an item to cart |
 | PATCH | `/v1/carts/{id}/items/{variantID}` | Update item quantity |
 | DELETE | `/v1/carts/{id}/items/{variantID}` | Remove item from cart |
  | POST | `/v1/checkout` | Place an order (requires Idempotency-Key header) |
  | POST | `/v1/webhooks/paystack` | Paystack webhook endpoint |
 | GET | `/v1/orders/{id}` | Get order details |
 | POST | `/v1/orders/{id}/refund` | Refund order (requires `Idempotency-Key`) |
 | POST | `/v1/admin/keys` | Create API key (admin only) |
 | GET | `/v1/admin/keys` | List API keys (admin only) |
 | DELETE | `/v1/admin/keys/{id}` | Revoke API key (admin only) |
 | GET | `/v1/admin/products` | List products (admin only) |
 | POST | `/v1/admin/products` | Create product (admin only) |
 | GET | `/v1/admin/products/{id}` | Get product (admin only) |
 | PATCH | `/v1/admin/products/{id}` | Update product (admin only) |
 | DELETE | `/v1/admin/products/{id}` | Delete product (admin only) |
 | PUT | `/v1/admin/products/{id}/stock` | Update stock level (admin only) |
 | GET | `/v1/admin/orders` | List orders (admin only) |
 | GET | `/v1/admin/orders/{id}` | Get order (admin only) |

## API Key Authentication

Admin endpoints and partner surfaces use scoped API keys for authentication. API keys use the `Authorization` header with the Bearer scheme:

```
Authorization: Bearer kik_<key>
```

### Scopes

API keys are scoped to specific permissions:

| Scope | Description |
|-------|-------------|
| `products:read` | Read product catalog |
| `orders:read` | Read order information |
| `orders:write` | Create and modify orders |
 | `stock:read` | Read inventory levels |
 | `stock:write` | Modify inventory |
 | `admin:read` | Read admin data |
 | `admin:write` | Perform admin operations |

### Creating API Keys

Create a new API key:

```bash
curl -X POST http://localhost:8080/v1/admin/keys \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kik_admin_key" \
  -d '{
    "name": "Integration Key",
    "scopes": ["products:read", "orders:write"]
  }'
```

Response:
```json
{
  "id": "key_abc123...",
  "name": "Integration Key",
  "prefix": "kik_abc1",
  "key": "kik_abc123def456...",
  "scopes": ["products:read", "orders:write"]
}
```

**Important**: The `key` field is shown **only once** when created. Store it securely; only the SHA-256 hash is stored.

## Admin API

The admin API provides a storefront-safe interface for managing products, variants, prices, and stock. Unlike the public storefront API, the admin API exposes internal IDs, stock counts by location, and enables mutation operations.

### Admin Scopes

Admin endpoints require specific scopes:

| Endpoint | Required Scope |
|----------|---------------|
| GET /v1/admin/products | `admin:read` |
| POST /v1/admin/products | `admin:write` |
| PATCH /v1/admin/products/{id} | `admin:write` |
| DELETE /v1/admin/products/{id} | `admin:write` |
| PUT /v1/admin/products/{id}/stock | `admin:write` |
| GET /v1/admin/orders | `admin:read` |
| GET /v1/admin/orders/{id} | `admin:read` |

### Creating API Keys

Create a new API key:

```bash
curl -X POST http://localhost:8080/v1/admin/keys \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kik_admin_key" \
  -d '{
    "name": "Integration Key",
    "scopes": ["products:read", "orders:write"]
  }'
```

Response:
```json
{
  "id": "key_abc123...",
  "name": "Integration Key",
  "prefix": "kik_abc1",
  "key": "kik_abc123def456...",
  "scopes": ["products:read", "orders:write"]
}
```

**Important**: The `key` field is shown **only once** when created. Store it securely; only the SHA-256 hash is stored.

### Listing API Keys

List all keys (shows prefix and scopes only, no key material):

```bash
curl -X GET http://localhost:8080/v1/admin/keys \
  -H "Authorization: Bearer kik_admin_key"
```

### Revoking API Keys

Revoke (not delete) a key:

```bash
curl -X DELETE http://localhost:8080/v1/admin/keys/key_abc123... \
  -H "Authorization: Bearer kik_admin_key"
```

### Authentication Errors

| Status | Code | Description |
|--------|------|-------------|
| 401 | `missing_key` | No `Authorization` header provided |
| 401 | `invalid_key` | Key not found or invalid format |
| 401 | `expired_key` | Key has expired |
- **401 | `revoked_key` | Key has been revoked |
| 403 | `missing_scope` | Key lacks required scope (includes `required_scope` in response) |
| 429 | `rate_limited` | Rate limit exceeded (includes `Retry-After` header) |
| 413 | `request_too_large` | Request body exceeds limit |

### Rate Limiting

Kikapu implements token bucket rate limiting to prevent abuse and resource exhaustion. Rate limits are configurable per endpoint type.

#### Configuration

Environment variables:

- `KIKAPU_RATE_LIMIT_CHECKOUT` - Maximum checkout requests per minute per API key (default: 10)
- `KIKAPU_RATE_LIMIT_ADMIN` - Maximum admin requests per minute per API key (default: 120)
- `KIKAPU_RATE_LIMIT_PUBLIC_READ` - Maximum public read requests per minute per IP address (default: 300)
- `KIKAPU_RATE_LIMIT_MAX_BYTES` - Maximum request body size in bytes (default: 1048576 = 1 MiB)
- `KIKAPU_RATE_LIMIT_TRUSTED_PROXIES` - Whether to trust X-Forwarded-For headers from proxies (default: false)

#### Enforcement

Rate limits are enforced:
- **Checkout endpoint** (`POST /v1/checkout`) - Uses API key from `Authorization` header
- **Admin endpoints** - Use API key from `Authorization` header
- **Public read endpoints** - Use client IP address from `X-Real-IP` or `X-Forwarded-For`

When rate limits are exceeded, clients receive `429 Too Many Requests` with a `Retry-After: 1` header indicating how long to wait before retrying.

#### X-Forwarded-For Security

The `X-Forwarded-For` header contains the client's original IP address and is set by reverse proxies or load balancers to indicate the real client IP. When `KIKAPU_RATE_LIMIT_TRUSTED_PROXIES=true`, Kikapu will:

1. Check the `X-Forwarded-For` header for the client's original IP
2. Use the last IP in the chain as the rate limit key
3. Igloo the first untrusted IP before the last one

**Security Note**: Always ensure your reverse proxies are configured to forward the correct `X-Forwarded-For` header with the client's original IP. Do not enable `KIKAPU_RATE_LIMIT_TRUSTED_PROXIES` unless you trust all upstream proxies to correctly set this header.

#### Request Size Limits

For JSON and upload endpoints (POST, PUT, PATCH), Kikapu enforces a maximum request body size of 1 MiB by default. Oversized requests receive `413 Request Entity Too Large`.

File uploads (5 MiB max) should use multipart/form-data and are handled separately from JSON payload limits.

### Security Notes

- Only the SHA-256 hash of the key is stored; the actual key is never persisted
- Keys appear only once, in the create response (store securely)
- Logs show only the key prefix (first 8 characters), never the full key
- Constant-time comparison prevents timing attacks
- `RevokedAt` takes precedence over `ExpiresAt`

## Admin API Examples

### Create a Product

Create a new product with variants. Requires `Idempotency-Key` header for exactly-once semantics:

```bash
curl -X POST http://localhost:8080/v1/admin/products \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kik_admin_key" \
  -H "Idempotency-Key: unique-request-key-12345" \
  -d '{
    "slug": "wireless-keyboard",
    "name": "Wireless Keyboard",
    "status": "draft",
    "variants": [
      {
        "sku": "WK-BLK-US",
        "price": 7999,
        "currency": "USD"
      },
      {
        "sku": "WK-WHT-US",
        "price": 7999,
        "currency": "USD"
      }
    ]
  }'
```

Response (201 Created):
```json
{
  "id": "prod_abc123...",
  "slug": "wireless-keyboard",
  "name": "Wireless Keyboard",
  "status": "draft",
  "variants": [
    {
      "id": "var_001",
      "product_id": "prod_abc123...",
      "sku": "WK-BLK-US",
      "price": {"Value": 7999, "Currency": "USD"},
      "created_at": "2024-01-15T10:30:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    },
    {
      "id": "var_002",
      "product_id": "prod_abc123...",
      "sku": "WK-WHT-US",
      "price": {"Value": 7999, "Currency": "USD"},
      "created_at": "2024-01-15T10:30:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    }
  ],
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:00Z"
}
```

#### Idempotency Behavior

- Same `Idempotency-Key` + same request → same response (replay returns the original product)
- Concurrent requests with same key → guaranteed single product creation
- Idempotency prevents duplicate products even under concurrent access

### Validation Errors

Invalid requests return a 400 with field-level errors:

```json
{
  "error": {
    "code": "validation_error",
    "message": "validation failed",
    "fields": {
      "slug": "invalid slug",
      "variants[0].price": "price cannot be negative"
    }
  }
}
```

Common validation errors:

| Field | Error | Description |
|-------|-------|-------------|
| `slug` | `invalid slug` | Must be lowercase alphanumeric with hyphens, 1-80 chars |
| `name` | `name cannot be empty` | Required field |
| `status` | `status must be draft or published` | Only `draft` or `published` allowed on create |
| `variants[i].sku` | `SKU cannot be empty` | Required field |
| `variants[i].price` | `price cannot be negative` | Must be >= 0 |
| `variants[i].currency` | `currency must be a valid ISO-4217 code` | Must be 3 uppercase letters |
| `variants[i].sku` | `duplicate SKU` | SKU already exists in product |

### Update a Product

Update product details and variants:

```bash
curl -X PATCH http://localhost:8080/v1/admin/products/prod_abc123 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kik_admin_key" \
  -d '{
    "slug": "wireless-keyboard-pro",
    "name": "Wireless Keyboard Pro",
    "status": "published",
    "variants": [
      {
        "sku": "WK-BLK-US",
        "price": 8999,
        "currency": "USD"
      }
    ]
  }'
```

**Note**: Updating variants replaces all variants. Include all variants you want to keep.

### Delete a Product (Soft Delete)

Deleting a product sets its status to `archived`. It will no longer appear in storefront listings but remains in the database for order history.

```bash
curl -X DELETE http://localhost:8080/v1/admin/products/prod_abc123 \
  -H "Authorization: Bearer kik_admin_key"
```

#### Delete Constraints

- **409 Conflict**: Cannot delete a product with existing orders. Returns:
  ```json
  {
    "error": {
      "code": "product_in_use",
      "message": "cannot delete product with existing orders"
    }
  }
  ```

### Update Stock Levels

Set stock level for a variant at a specific location:

```bash
curl -X PUT http://localhost:8080/v1/admin/products/var_001/stock \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kik_admin_key" \
  -d '{
    "location_id": "warehouse-us-east",
    "on_hand": 150
  }'
```

Response:
```json
{
  "variant_id": "var_001",
  "location_id": "warehouse-us-east",
  "on_hand": 150,
  "reserved": 10
}
```

#### Stock Validation

- **Negative `on_hand`**: Returns 400 validation error
- **Insufficient stock**: If `on_hand` < `reserved`, returns 400:
  ```json
  {
    "error": {
      "code": "insufficient_stock",
      "message": "on_hand (50) cannot be less than reserved (100)"
    }
  }
  ```

The `Available()` calculation (`on_hand - reserved`) gives the sellable quantity visible to the storefront.

### List Products (Admin)

List products with pagination and filtering:

```bash
curl -X GET "http://localhost:8080/v1/admin/products?status=published&limit=20" \
  -H "Authorization: Bearer kik_admin_key"
```

Query parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `status` | string | Filter by status: `draft`, `published`, or `archived` |
| `limit` | integer | Max results (1-100, default: 20) |
| `cursor` | string | Pagination cursor from previous response |

Response:
```json
{
  "data": [
    {
      "id": "prod_abc123",
      "slug": "wireless-keyboard",
      "name": "Wireless Keyboard",
      "status": "published",
      "variants": [...]
    }
  ],
  "next_cursor": "eyJpZCI6InByb2RfYWJjMTIzIn0="
}
```

### Admin Order Endpoints

List and retrieve orders for admin purposes:

```bash
# List orders with filters
curl -X GET "http://localhost:8080/v1/admin/orders?status=confirmed&from=2024-01-01T00:00:00Z" \
  -H "Authorization: Bearer kik_admin_key"

# Get single order
curl -X GET http://localhost:8080/v1/admin/orders/ord_123 \
  -H "Authorization: Bearer kik_admin_key"
```

Query parameters for listing orders:

| Parameter | Type | Description |
|-----------|------|-------------|
| `status` | string | Filter by order status |
| `from` | RFC3339 timestamp | Filter orders from this time |
| `to` | RFC3339 timestamp | Filter orders to this time |
| `limit` | integer | Max results (1-100, default: 20) |
| `cursor` | string | Pagination cursor |

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

### Filter Products by Region and Channel

Products can be filtered by region and channel using query parameters:

```bash
curl "http://localhost:8080/v1/products?region=reg-001&channel=default"
```

This filters products based on:
- Region: Returns products available in the specified region's currency
- Channel: Returns products published in the specified sales channel

### Regions

List all available regions:

```bash
curl http://localhost:8080/v1/regions
```

Response:
```json
{
  "data": [
    {
      "id": "reg-001",
      "name": "Kenya",
      "currency": "KES",
      "countries": ["KE"],
      "tax_rate_bps": 1600,
      "tax_inclusive": true
    }
  ]
}
```

### Sales Channels

List all sales channels:

```bash
curl http://localhost:8080/v1/channels
```

Response:
```json
{
  "data": [
    {
      "id": "ch-001",
      "name": "Default",
      "slug": "default",
      "regions": ["reg-001", "reg-002"]
    }
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
 
## Cart API Examples

### Create a Cart

```bash
curl -X POST http://localhost:8080/v1/carts \
  -H "Content-Type: application/json" \
  -d '{"currency": "USD"}'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "currency": "USD",
  "lines": [],
  "version": 1,
  "updated_at": "2024-01-15T10:30:00Z"
}
```

### Add an Item to Cart

```bash
curl -X POST http://localhost:8080/v1/carts/{cart_id}/items \
  -H "Content-Type: application/json" \
  -d '{"variant_id": "var001", "quantity": 2, "unit_price": 2999}'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "currency": "USD",
  "lines": [
    {
      "variant_id": "var001",
      "quantity": 2,
      "unit_price": {"value": 2999, "currency": "USD"}
    }
  ],
  "version": 2,
  "updated_at": "2024-01-15T10:31:00Z"
}
```

### Update Item Quantity

```bash
curl -X PATCH http://localhost:8080/v1/carts/{cart_id}/items/var001 \
  -H "Content-Type: application/json" \
  -d '{"quantity": 5}'
```

### Remove an Item

```bash
curl -X DELETE http://localhost:8080/v1/carts/{cart_id}/items/var001
```

### Delete Cart

```bash
curl -X DELETE http://localhost:8080/v1/carts/{cart_id}
```

## Checkout API Example

### Place an Order (with Idempotency)

The checkout endpoint requires an `Idempotency-Key` header to ensure idempotent operations. Submitting the same key multiple times will return the same order without creating duplicates.

```bash
curl -X POST http://localhost:8080/v1/checkout \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: unique-request-key-12345" \
  -d '{"cart_id": "550e8400-e29b-41d4-a716-446655440000"}'
```

Response:
```json
{
  "order_id": "ord_1705315200",
  "status": "paid"
}
 ```

#### Idempotency Behavior

- Same `Idempotency-Key` + same request → same response (replay returns the original response)
- Same `Idempotency-Key` + different request → 409 Conflict (key reused with different payload)
- Concurrent requests with same key → 409 Conflict with `Retry-After` header (request in flight)
- Idempotency records expire after 24 hours
- Expired records are cleaned up by a background worker

#### Idempotency Error Responses

**Key reused with different request (409 Conflict):**
```json
{
  "error": {
    "code": "idempotency_conflict",
    "message": "This idempotency key was already used with a different request"
  }
}
```

**Request in flight (409 Conflict):**
```json
{
  "error": {
    "code": "request_in_flight",
    "message": "A request with this idempotency key is already being processed. Retry after the indicated time."
  }
}
```

The response includes a `Retry-After: 30` header indicating when to retry.

#### Error Cases

**Missing Idempotency-Key:**
```bash
curl -X POST http://localhost:8080/v1/checkout \
  -H "Content-Type: application/json" \
  -d '{"cart_id": "550e8400-e29b-41d4-a716-446655440000"}'
```

Response (400 Bad Request):
```json
{
  "error": {
    "code": "missing_idempotency_key",
    "message": "Idempotency-Key header is required"
  }
}
```

**Empty Cart:**
```json
{
  "error": {
    "code": "empty_cart",
    "message": "cart is empty"
  }
 }
 ```
 
 ## Payment Providers
  
 Kikapu supports multiple payment providers. Currently implemented:
 
 ### Fake Provider (Default)
 
 The fake provider is used by default for development and testing. It simulates successful payments without making real API calls.
 
 ### Paystack Provider
 
 Paystack is a payment gateway for African businesses. To use Paystack:
 
 1. Set environment variables:
  ```bash
  export PAYMENT_PROVIDER=paystack
  export PAYSTACK_SECRET_KEY=sk_test_your_secret_key
  ```
  
 2. Configure webhook in your Paystack dashboard:
  - URL: `https://your-domain.com/v1/webhooks/paystack`
  - Events: `charge.success`
  
 #### Webhook Contract
  
 The Paystack webhook endpoint (`POST /v1/webhooks/paystack`):
  
 - **Signature Verification**: Validates `X-Paystack-Signature` header using HMAC-SHA512
 - **Idempotency**: Guarantees single state transition per event (by event ID and transaction reference)
 - **Event Types**: 
  - `charge.success` - Processed with state transition
  - Unknown events - Returns 200 OK but no state change (logged)
  
 #### Paystack Status Mapping
  
 | Paystack Status | Kikapu State |
 |----------------|--------------|
 | `success` | `captured` |
 | `failed` | `failed` |
 | `abandoned` | `pending` |
  
 #### Webhook Response Codes
  
 | Status | Condition |
 |--------|-----------|
 | 200 OK | Valid signature, event processed or ignored |
 | 400 Bad Request | Invalid payload |
 | 401 Unauthorized | Missing or invalid signature |
 
 ### M-Pesa Provider
 
 M-Pesa (Daraja API) is Safaricom's mobile payment platform for Kenya. To use M-Pesa:
 
 1. Set environment variables:
  ```bash
  export PAYMENT_PROVIDER=mpesa
  export MPESA_CONSUMER_KEY=your_consumer_key
  export MPESA_CONSUMER_SECRET=your_consumer_secret
  export MPESA_SHORT_CODE=174379
  export MPESA_PASSKEY=your_passkey
  export MPESA_CALLBACK_URL=https://your-domain.com/v1/webhooks/mpesa
  export MPESA_ALLOWED_CIDRs=196.216.200.0/24
  ```
 
 **Important**: M-Pesa amounts are in **integer KES** (no cents). For example, 100 KES is represented as `100`, not `10000`.
 
 #### STK Push Flow
 
 1. Customer initiates payment
 2. `Charge()` sends STK push to customer's phone
 3. Customer enters M-Pesa PIN
 4. Callback received with payment result
 5. Use `Verify()` to check status if callback not received
 
 #### Callback Contract
 
 The M-Pesa callback endpoint receives `POST` requests with the following payload:
 
 ```json
 {
   "Body": {
     "stkCallback": {
       "MerchantRequestID": "12345",
       "CheckoutRequestID": "ws_CO_123456",
       "ResultCode": 0,
       "ResultDesc": "Success",
       "CallbackMetadata": {
         "Item": [
           {"Name": "Amount", "Value": 100},
           {"Name": "MpesaReceiptNumber", "Value": "QEQ123"},
           {"Name": "PhoneNumber", "Value": 254712345678},
           {"Name": "TransactionDate", "Value": 20240115103000}
         ]
       }
     }
   }
 }
 ```
 
 - **Idempotency**: Callbacks are idempotent by `CheckoutRequestID` + `ResultCode`
 - **Duplicate callbacks**: No-op, returns 200 OK
 - **Missing fields**: Returns 400 Bad Request with error envelope
 
 #### IP Allowlist (Critical Security)
 
 **M-Pesa callbacks are NOT signed by Safaricom.** The only security control is IP allowlisting:
 
 - In **production mode**, callbacks will be rejected if `MPESA_ALLOWED_CIDRS` is not configured
 - In **development mode**, callbacks are accepted without IP validation (but logging occurs)
 - Configure Safaricom's IP ranges in `MPESA_ALLOWED_CIDRS` (comma-separated CIDRs)
 - Example: `MPESA_ALLOWED_CIDRS=196.216.200.0/24,196.216.201.0/24`
 
 #### M-Pesa Result Code Mapping
 
 | Result Code | Meaning | Kikapu State |
 |------------|---------|--------------|
 | 0 | Success | `captured` |
 | 1032 | Cancelled by user | `failed` |
 | 1037 | Timeout | `failed` |
 | Other | Various errors | `failed` |
 
 #### Phone Number Format
 
 Phone numbers should be in international format: `2547XXXXXXXX`
 
 Automatic normalization:
 - `0712345678` → `254712345678`
 - `+254712345678` → `254712345678`

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

## Inventory & Reservations

Kikapu uses a reservation-based inventory system to prevent overselling under concurrent access.

### Reservation Lifecycle

Every reservation follows one of three paths:

```
reserve → commit   (sale completed, stock removed)
reserve → release  (customer cancelled, stock returned)
reserve → expire   (timeout, stock returned automatically)
```

### Checkout Flow

The checkout process enforces strict ordering to guarantee no overselling:

1. **Reserve inventory** before charging payment
2. **Commit** reservations on successful payment capture
3. **Release** reservations on payment failure or cancellation
4. **Expire** reservations via background process

This contract ensures inventory is never oversold, even under high concurrency (proven by the 100-goroutine race test).

### Stock Locations

Inventory is tracked per variant per location:

```json
{
  "variant_id": "var001",
  "location_id": "warehouse-ke",
  "on_hand": 100,
  "reserved": 10
}
```

The `Available()` calculation (`on_hand - reserved`) gives the sellable quantity.

## Order Lifecycle

Orders in Kikapu follow a strict state machine with guarded transitions and immutable totals after payment capture.

### Status Transitions

| from | to | allowed |
|---|---|---|
| pending | confirmed, cancelled | yes |
| confirmed | fulfilled, cancelled, refunded | yes |
| fulfilled | refunded | yes |
| cancelled, refunded | — | terminal |

### Payment Status

Payment status tracks the financial state of an order:

- `pending`: Payment not yet processed
- `captured`: Payment successfully captured (totals locked)
- `partially_refunded`: Some amount refunded
- `refunded`: Full amount refunded

### Refund Rules

- Partial refunds are allowed
- Cumulative refund amount must not exceed captured amount
- Each refund gets a unique ID and timestamp
- Two partial refunds that equal the total mark the order as `refunded`
- A third refund attempt that exceeds captured amount is rejected with `ErrRefundExceedsCaptured`

### Order Numbers

Order numbers follow the format `KIK-000123`:
- Sequential per region
- Generated by `Counter` interface
- In-memory implementation for current version

### Totals Calculation

Order totals are computed once and locked after payment capture:

```
GrandTotal = Subtotal + Tax - Discount
```

All monetary values are stored in minor units (cents, pence, etc.) using the `money.Amount` type.

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/orders/{id}` | Get order details (storefront) |
| POST | `/v1/orders/{id}/refund` | Refund order (admin, requires `Idempotency-Key`) |

#### Refund Example

```bash
curl -X POST http://localhost:8080/v1/orders/ord-001/refund \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: unique-refund-key-123" \
  -d '{"amount": {"value": 5000, "currency": "USD"}, "reason": "Customer request"}'
```

Response:
```json
{
  "id": "ref_20240115103000abc123",
  "amount": {"value": 5000, "currency": "USD"},
  "reason": "Customer request",
  "created_at": "2024-01-15T10:30:00Z"
}
```

## Shipping Zones & Rates

Kikapu supports shipping zones with multiple rate types and deterministic rate selection at checkout.

### Zones

Shipping zones are defined per region and country:

```go
type Zone struct {
    ID        string
    Name      string
    Countries []string
}
```

Example:
```go
zone := Zone{
    ID:        "east-africa",
    Name:      "East Africa",
    Countries: []string{"KE", "UG", "TZ"},
}
```

### Rate Types

Three rate types are supported:

1. **Flat Rate**: Fixed shipping cost regardless of weight
   ```go
   Rate{Kind: KindFlat, Amount: money.NewAmount(500, "USD")}
   ```

2. **Weight-Based**: Based on total cart weight (grams)
   ```go
   Rate{Kind: KindWeight, Amount: money.NewAmount(1000, "USD"), MinWeightG: 2000}
   ```

3. **Free Over Threshold**: Free shipping when subtotal meets threshold
   ```go
   Rate{Kind: KindFreeOver, FreeOver: money.NewAmount(10000, "USD")}
   ```

### Rate Selection Rules

Rates are selected with deterministic ordering:

1. **Free-over rates win** when subtotal ≥ threshold (equal threshold qualifies)
2. **Ties broken by cheapest**, then by RateID (lexicographic)
3. **Unknown destination** returns `ErrNoShippingZone`

Example for KE/UG/TZ:
```go
zones := []Zone{
    {ID: "east-africa", Countries: []string{"KE", "UG", "TZ"}},
    {ID: "west-africa", Countries: []string{"NG", "GH"}},
}
rates := []Rate{
    {ID: "free", ZoneID: "east-africa", Kind: KindFreeOver, FreeOver: money.NewAmount(10000, "USD")},
    {ID: "flat", ZoneID: "east-africa", Kind: KindFlat, Amount: money.NewAmount(500, "USD")},
    {ID: "heavy", ZoneID: "east-africa", Kind: KindWeight, Amount: money.NewAmount(800, "USD"), MinWeightG: 5000},
}

matcher := shipping.NewZoneMatcher(zones)
zone, err := matcher.FindZone("KE") // Returns east-africa

// For cart with subtotal 10000 USD
applicable, _ := shipping.RatesFor(zone, rates, cart, subtotal, "USD")
// Returns: [free(0 USD), flat(500 USD), heavy(800 USD)]
```

### Deterministic Ordering

When multiple rates apply:
- Free-over rates always appear first (when threshold met)
- Remaining rates sorted by amount (ascending)
- Equal amounts sorted by ID (lexicographic)

## Architecture Invariants

- Single binary deployment
- No in-process state as source of truth (state lives in Postgres)
- Money represented in minor units (cents, pence, etc.)
- All mutations are idempotent where possible
- Small, focused commits

## Promotions

Kikapu supports percentage, fixed-amount, and free shipping promotions with deterministic stacking rules.

### Stacking Order

When multiple promotions apply, they are applied in a specific order:

1. **Percentage promotions first** (applied to subtotal)
2. **Fixed-amount promotions second** (applied after percentage discounts)
3. **Ties broken by ID** (lexicographic order when multiple promotions of the same type exist)

This order ensures predictable, deterministic discount calculations.

### Single-Code Rule

- **At most one code-based promotion per order**
- Automatic promotions (no code required) can stack without limit
- If multiple code promotions are provided, the first valid one is applied and subsequent ones are rejected with reason `one_code_per_order`

### Promotion Application

Promotions are validated before application:
- Must be within the active date range (`StartsAt <= now < EndsAt`)
- Must have remaining usage (if `UsageLimit` is set)
- Must meet minimum subtotal requirement (if `MinSubtotal` is set)
- Can target specific variants via `AppliesTo` (empty = all variants)
- Rejected promotions never partially apply

### Example

```go
// 10% off + 50 KES fixed discount on a 1000 KES order
promotions := []Promotion{
    {ID: "p1", Kind: KindPercentage, Value: 1000}  // 1000 bps = 10%
    {ID: "p2", Kind: KindFixed, Value: 5000}       // 5000 minor units = 50 KES
}
applied, rejected := Apply(lines, promotions, time.Now())
// Result: 10% off 1000 = 100 KES, then 50 KES fixed = 150 KES total discount
```

## Tax Calculation

Kikapu calculates VAT/sales tax with cent-level accuracy using round-half-even (banker's rounding).

### Rounding Rule

- Per-line tax amounts are rounded using **round-half-even** (banker's rounding)
- After rounding, a **residual adjustment** is applied to ensure the sum of per-line tax equals the total exactly
- The adjustment is applied to the line with the largest tax amount

### Inclusive vs Exclusive Tax

- **Exclusive**: Tax is added on top of the line total (`tax = subtotal × rate`)
- **Inclusive**: Tax is extracted from the total (`tax = total × rate / (10000 + rate)`)

Tax rates are expressed in **basis points**:
- 1600 bps = 16%
- 2000 bps = 20%

### Example: 16% VAT on 999 KES

```go
// 999 KES at 16% VAT (exclusive)
lines := []order.Line{
    {UnitPrice: money.NewAmount(99900, "KES"), Quantity: 1}  // 999.00 KES
}
perLine, total := TaxFor(lines, 1600, false)
// 99900 * 1600 / 10000 = 15984 (159.84 KES)
// Round-half-even: 159.84 KES (already an integer in minor units)
```

### Example: 3-Line Order with Residual

```
Line 1: 33.33 KES × 16% = 5.3328 → rounded to 5.33
Line 2: 33.33 KES × 16% = 5.3328 → rounded to 5.33
Line 3: 33.34 KES × 16% = 5.3344 → rounded to 5.33
Sum: 15.99, Expected total: 16.00 → Residual: 0.01 added to Line 3 (largest)
Final: [5.33, 5.33, 5.34] = 16.00 ✓
```

## Event System & Webhooks

Kikapu provides a transactional outbox pattern for reliable event delivery. Every state change emits an event in the same transaction, ensuring data consistency.

### Event Types

| Event Type | Description | Trigger |
|------------|-------------|---------|
| `order.created` | Order placed successfully | Checkout completion |
| `order.paid` | Payment captured for order | Successful payment |
| `order.fulfilled` | Order marked as fulfilled | Fulfillment operation |
| `order.refunded` | Refund processed for order | Refund operation |
| `stock.reserved` | Inventory reserved | Stock reservation |
| `stock.released` | Inventory released | Reservation release/expiry |

### Event Envelope

All events follow this structure:

```json
{
  "id": "evt_abc123def456",
  "type": "order.paid",
  "version": 1,
  "aggregate_id": "ord_123",
  "occurred_at": "2024-01-15T10:30:00Z",
  "data": {
    "order_id": "ord_123",
    "payment_id": "pay_abc",
    "total": 10000,
    "currency": "USD",
    "captured_at": "2024-01-15T10:30:00Z"
  }
}
```

### Webhook Delivery

Events are delivered to configured subscribers via HTTP POST requests with the following headers:

| Header | Description |
|--------|-------------|
| `X-Kikapu-Event` | Event type (e.g., `order.paid`) |
| `X-Kikapu-Delivery` | Unique delivery ID for this attempt |
| `X-Kikapu-Signature` | Signature for verification |
| `Content-Type` | `application/json` |

### Signature Verification

Webhook signatures use HMAC-SHA256. The signature header format:

```
t=1705315200,v1=9f2c...
```

To verify a webhook:

1. Parse the header to extract `t` (timestamp) and `v1` (signature)
2. Create the signed payload: `timestamp + "." + request_body`
3. Compute HMAC-SHA256 with your webhook secret
4. Compare using constant-time comparison
5. Reject timestamps older than 5 minutes

```go
import "github.com/kyambuthia/kikapu/internal/webhook"

func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    sigHeader := r.Header.Get("X-Kikapu-Signature")
    
    err := webhook.Verify(webhookSecret, sigHeader, body, 5*time.Minute)
    if err != nil {
        // Handle invalid signature
        http.Error(w, "Invalid signature", 401)
        return
    }
    
    // Process event...
}
```

### Retry Schedule

Failed deliveries are retried with exponential backoff:

| Attempt | Delay |
|---------|-------|
| 1 | 1 second |
| 2 | 5 seconds |
| 3 | 30 seconds |
| 4+ | 5 minutes (capped) |

After 8 failed attempts, the event is moved to dead-letter status.

### Response Handling

- **2xx**: Delivery marked as sent, no retry
- **4xx**: Immediate dead-letter (client error)
- **5xx/timeout**: Retry with backoff

### Transactional Guarantee

Events are appended to the outbox in the same transaction as the state change. This ensures:

- If the order write fails, no event is created
- If payment fails, no events are emitted
- Events cannot be lost due to delivery failures

### Subscribing to Events

Configure subscriptions to receive specific event types:

```go
store := worker.NewInMemorySubscriberStore()
store.Add(worker.Subscription{
    ID:     "sub_123",
    URL:    "https://example.com/webhooks",
    Secret: "your-webhook-secret",
    Types:  []string{"order.paid", "order.refunded"},
    Active: true,
})
```

Use `"*"` as an event type to subscribe to all events.

## License

MIT
