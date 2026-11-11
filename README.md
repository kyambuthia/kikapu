# kikapu

A headless commerce engine in Go - single binary, Postgres backend, REST API with OpenAPI spec.

## Overview

Kikapu is designed as a lightweight, single-binary commerce engine that can run in API or worker mode. It uses PostgreSQL for persistence and exposes a RESTful API.

**Documentation**: [Architecture](docs/ARCHITECTURE.md) | [Roadmap](docs/ROADMAP.md) | [Engineering Standards](docs/STANDARDS.md)

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

## API Endpoints

 | Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Health check - returns `{"status":"ok"}` |
| GET | `/readyz` | Readiness check - returns 200 |
| GET | `/v1/version` | Version info - returns version and commit |
| GET | `/v1/products` | List products with pagination |
| GET | `/v1/products/{slug}` | Get a single product by slug |
| POST | `/v1/carts` | Create a new cart |
| GET | `/v1/carts/{id}` | Get a cart by ID |
| DELETE | `/v1/carts/{id}` | Delete (clear) a cart |
| POST | `/v1/carts/{id}/items` | Add an item to cart |
| PATCH | `/v1/carts/{id}/items/{variantID}` | Update item quantity |
| DELETE | `/v1/carts/{id}/items/{variantID}` | Remove item from cart |
 | POST | `/v1/checkout` | Place an order (requires Idempotency-Key header) |
| POST | `/v1/webhooks/paystack` | Paystack webhook endpoint |

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

- Same `Idempotency-Key` + same cart → same order returned
- Concurrent requests with same key → guaranteed single order creation
- Idempotency prevents duplicate charges even under concurrent access
- Cart is cleared after successful order

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

## Architecture Invariants

- Single binary deployment
- No in-process state as source of truth (state lives in Postgres)
- Money represented in minor units (cents, pence, etc.)
- All mutations are idempotent where possible
- Small, focused commits

## License

MIT
