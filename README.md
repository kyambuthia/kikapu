# kikapu

A headless commerce engine in Go - single binary, Postgres backend, REST API with OpenAPI spec.

## Overview

Kikapu is designed as a lightweight, single-binary commerce engine that can run in API or worker mode. It uses PostgreSQL for persistence and exposes a RESTful API.

**Documentation**: [Architecture](docs/ARCHITECTURE.md) | [Invariants](docs/INVARIANTS.md) | [Roadmap](docs/ROADMAP.md) | [Standards](docs/STANDARDS.md) | [ADRs](docs/adr/)

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
| 401 | `revoked_key` | Key has been revoked |
| 403 | `missing_scope` | Key lacks required scope (includes `required_scope` in response) |

### Security Notes

- Only the SHA-256 hash of the key is stored; the actual key is never persisted
- Keys appear only once, in the create response (store securely)
- Logs show only the key prefix (first 8 characters), never the full key
- Constant-time comparison prevents timing attacks
- `RevokedAt` takes precedence over `ExpiresAt`

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
