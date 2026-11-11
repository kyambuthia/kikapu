# Architecture

This document describes kikapu's current architecture. Every statement is verified against the codebase at HEAD; aspirational features are marked `planned`.

## 1. Context (C4 Level 1)

Kikapu is a headless commerce engine exposing a REST API for storefronts, mobile apps, admin dashboards, and partner integrations.

```
┌─────────────────────────────────────────────────────────────────┐
│                        External Systems                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐   │
│  │Storefront│  │ Mobile   │  │  Admin    │  │   Partner    │   │
│  │  (Web)   │  │   App    │  │ Dashboard │  │  Integration │   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬───────┘   │
└───────┼─────────────┼─────────────┼───────────────┼───────────┘
        │             │             │               │
        └─────────────┴──────┬──────┴───────────────┘
                             │ HTTP/REST
                             ▼
                    ┌────────────────┐
                    │                │
                    │    kikapu      │
                    │  (api worker)  │
                    │                │
                    └───┬────────┬───┘
                        │        │
         ┌──────────────┘        └──────────────┐
         │                                      │
         ▼                                      ▼
  ┌──────────────┐                    ┌─────────────────┐
  │  PostgreSQL  │                    │    Payment      │
  │  (planned)   │                    │   Providers     │
  └──────────────┘                    │  ┌───────────┐  │
                                      │  │ Paystack  │  │
                                      │  └───────────┘  │
                                      │  ┌───────────┐  │
                                      │  │  M-Pesa   │  │
                                      │  └───────────┘  │
                                      │  ┌───────────┐  │
                                      │  │   Fake    │  │
                                      │  └───────────┘  │
                                      └─────────────────┘
                                             │
                                             ▼
                                      ┌─────────────────┐
                                      │    Webhook      │
                                      │   Consumers     │
                                      └─────────────────┘
```

**Current state**: In-memory stores, no Postgres. Webhook consumers consume payment callbacks (Paystack, M-Pesa).

## 2. Containers (C4 Level 2)

```
┌─────────────────────────────────────────────────────────────┐
│                      Docker Image                            │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │          Single Go Binary (kikapu)                    │  │
│  │                                                        │  │
│  │   ┌──────────────┐          ┌──────────────────┐     │  │
│  │   │  API Mode    │          │   Worker Mode    │     │  │
│  │   │  (default)   │          │    (planned)     │     │  │
│  │   │              │          │                  │     │  │
│  │   │ - HTTP API   │          │ - Event poller   │     │  │
│  │   │ - REST routes│          │ - Webhook retry  │     │  │
│  │   └──────┬───────┘          └────────┬─────────┘     │  │
│  │          │                           │                │  │
│  │          └───────────┬───────────────┘                │  │
│  │                      │                                │  │
│  │                      ▼                                │  │
│  │     ┌────────────────────────────────┐               │  │
│  │     │      Shared Domain Logic         │              │  │
│  │     │  - catalog - pricing - inventory │              │  │
│  │     │  - cart     - checkout - payment │              │  │
│  │     └────────────────────────────────┘               │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                              │
│  Data Stores:                                               │
│  - In-memory (current): cart, inventory, idempotency        │
│  - PostgreSQL (planned): all domain state                   │
└─────────────────────────────────────────────────────────────┘

No message broker. No cache layer. No microservices.
```

**Deployment**: Same binary, same container image. Mode controlled by `KIKAPU_MODE=api|worker`.

## 3. Module Map

| Package | Owns (tables/types) | May Import | Must Never Import |
|---------|---------------------|------------|-------------------|
| `internal/platform/config` | Config struct, env vars | stdlib | All business packages |
| `internal/platform/httpx` | ErrorEnvelope, RequestID middleware | stdlib | Domain types |
| `internal/platform/money` | Amount{Value,Currency}, arithmetic errors | stdlib | Business logic |
| `internal/platform/obs` | Logger interface, JSON handler | stdlib, log/slog | Request processing |
| `internal/region` | Region, Repository interface | stdlib | channel, pricing, catalog |
| `internal/channel` | SalesChannel, Repository interface | stdlib | region, pricing, catalog |
| `internal/catalog` | Product, Variant, Repository interface | money, httpx (error codes) | cart, checkout, order, payment |
| `internal/cart` | Cart, Line, Store interface | money, httpx | checkout, payment, inventory |
| `internal/inventory` | Reservation, Service, Clock abstraction | stdlib | cart, checkout, order, payment |
| `internal/pricing` | PriceResolver, Channel pricing, ResolveForRegion | money, catalog, region, channel | cart mutation, payment |
| `internal/payment` | Provider interface, Payment state machine, ChargeRequest/Result | money | HTTP, routes |
| `internal/payment/paystack` | Paystack API client, webhook signature verification | payment | M-Pesa, cart, checkout |
| `internal/payment/mpesa` | M-Pesa STK Push, callback handler, IP allowlist | payment | Paystack, cart, checkout |
| `internal/checkout` | Order, Service, IdempotencyStore | cart, inventory, payment | HTTP, routes |
| `internal/routes` | HTTP handlers, request/response DTOs | catalog, region, channel, cart, checkout, httpx | Database SQL, payment implementation details |

**Import graph**: `routes → domain services → platform utilities`

## 4.1. Regions and Sales Channels

Kikapu supports Medusa-style regions and sales channels for multi-currency, multi-region pricing.

### Region

A region defines:
- Currency (ISO-4217, e.g., "KES", "USD")
- Countries (ISO-3166 alpha-2, e.g., "KE", "UG")
- Tax rate in basis points (0-10000, where 1600 = 16%)
- Tax inclusive/exclusive flag

### Sales Channel

A sales channel defines:
- ID, Name, and Slug
- List of region IDs where products are available

### Product Visibility

Products are visible per channel and priced per region:
- A product must be published in a channel (region association)
- Pricing is resolved based on the region's currency

### Pricing Resolution Flow

```
1. Base price (from product variant)
   ↓
2. Price list for the region (planned)
   ↓
3. Promotion (planned)
   ↓
4. Tax calculation (inclusive/exclusive based on region)
```

The resolution order ensures predictable pricing across regions.

## 4. Request Path: POST /v1/checkout

```
1. HTTP Request (POST /v1/checkout)
   ↓
2. RequestIDMiddleware (inject/generate X-Request-ID)
   ↓
3. LoggingMiddleware (start timer)
   ↓
4. CartHandler.Checkout (routes/cart.go:238)
   - Extract Idempotency-Key header (line 239)
   - Parse JSON body → CheckoutRequest (line 246)
   ↓
5. checkout.Service.PlaceOrder (checkout/checkout.go:125)
   ↓
6. IdempotencyStore.GetOrderForKey (line 130)
   - If exists: return cached order (line 131-132)
   ↓
7. cart.Store.Get (line 134)
   - Load cart by ID
   - Return ErrCartNotFound if missing
   ↓
8. cart.Total() (line 143)
   - Sum line totals: unit_price × quantity
   - Money arithmetic with overflow checks
   ↓
9. inventory.Service.Reserve (line 151)
   - For each line item:
     - Lock mutex (line 69)
     - Check available - reserved ≥ quantity (line 80)
     - Create reservation with TTL (line 84-93)
     - On failure: release all reservations (line 153-156)
   ↓
10. payment.Provider.Charge (line 161)
    - Send to payment gateway (Paystack/M-Pesa/Fake)
    - On decline: ErrChargeFailed
    - On timeout: ErrTimeout
    - On success: ChargeResult{ID, Status}
    ↓
11. On payment failure:
    - inventory.Service.Release (line 169)
    - Return error (line 171)
    ↓
12. Order creation (line 174-183)
    - Generate order ID
    - Set status to "paid"
    - Store payment ID, total, currency
    ↓
13. IdempotencyStore.SaveOrder (line 185)
    - Lock mutex (line 73)
    - Store key → order mapping
    - Store order by ID
    ↓
14. cart.Clear() (line 189)
    - Remove all lines
    - Increment version
    ↓
15. cart.Store.Save (line 190)
    - Persist cleared cart
    ↓
16. Return Order{ID, Status} (line 193)
    ↓
17. HTTP Response 200 OK
    {"order_id": "ord_123", "status": "paid"}
```

### Failure Path Table

| Failure Point | Error | Compensation | User Message |
|---------------|-------|--------------|--------------|
| Cart not found | `ErrCartNotFound` | None (no side effects yet) | "cart not found" |
| Empty cart | `ErrEmptyCart` | None | "cart is empty" |
| Insufficient stock | `ErrInsufficientStock` | None (reservation not created) | "insufficient stock" |
| Payment declined | `ErrChargeFailed` | Release all reservations (line 169) | "charge failed" |
| Payment timeout | `ErrTimeout` | Release all reservations (line 169) | "payment timeout" |
| Save order fails | Internal error | Release reservations, payment already captured | "failed to place order" |

**Compensation invariant**: Payment failures trigger reservation release. Inventory is reserved before payment; released on payment failure.

### Concurrency During Checkout

- **IdempotencyStore**: Protected by `sync.RWMutex`. Concurrent requests with same key return same order.
- **Inventory.Service**: Protected by `sync.Mutex`. Stock levels checked atomically.
- **Cart**: No mutex (handler serializes access per request).
- **Rule**: **No critical section performs I/O while holding a lock**. Locks guard in-memory maps only.

## 5. Concurrency Model

### What's Guarded by Mutex

| Package | Guarded State | Lock Type | Protected Operations |
|---------|---------------|-----------|----------------------|
| `checkout.IdempotencyStore` | `keys map[string]string`, `orders map[string]*Order` | `sync.RWMutex` | Get/Save order by idempotency key |
| `inventory.Service` | `stock map[string]int`, `reservations map[string]*Reservation` | `sync.RWMutex` | Reserve/Release stock |
| `payment.FakeProvider` | `payments map[string]*Payment`, `idempotencyKeys map[string]string` | `sync.Mutex` | Charge/GetPayment |

### What's Guarded by Optimistic Version

| Type | Field | Purpose |
|------|-------|---------|
| `cart.Cart` | `Version int` | Detect concurrent modifications. Incremented on every mutation (AddItem, UpdateQuantity, RemoveItem, Clear). |

**Current state**: Version is tracked but not enforced (in-memory store doesn't check versions on save).

### What Will Be Guarded by DB Constraint (Postgres)

| Table | Constraint | Enforcement |
|-------|------------|-------------|
| `inventory_stock` | `CHECK (available >= 0)` | Database rejects negative stock |
| `orders` | `PRIMARY KEY (id)` | Prevent duplicate order IDs |
| `idempotency_keys` | `UNIQUE (key)` | Prevent duplicate order creation |
| `carts` | `version` column with optimistic lock | `WHERE id = $1 AND version = $2` |

### Critical Section Rule

**INVARIANT**: No critical section that performs I/O while holding a lock.

```go
// CORRECT: Lock, modify map, unlock, then I/O
s.mu.Lock()
reservations[id] = r
s.mu.Unlock()
// Now safe to call payment provider

// WRONG: I/O while holding lock
s.mu.Lock()
reservations[id] = r
paymentProvider.Charge(req) // NETWORK I/O WITH LOCK HELD
s.mu.Unlock()
```

**Why**: I/O can block for seconds. Lock held during block blocks all other goroutines.

## 6. Error Taxonomy

| Code | HTTP Status | Retryable | Meaning | Test Coverage |
|------|-------------|-----------|---------|---------------|
| `product_not_found` | 404 | No | Product slug does not exist | `catalog_test.go` |
| `invalid_slug` | 400 | No | Product slug format invalid | `product_test.go` |
| `invalid_limit` | 400 | No | Pagination limit > 100 or < 1 | `catalog_test.go` |
| `invalid_cursor` | 400 | No | Pagination cursor malformed | Planned |
| `cart_not_found` | 404 | No | Cart ID does not exist | `cart_test.go` |
| `empty_cart` | 400 | No | Checkout on cart with no items | `checkout_test.go` |
| `invalid_quantity` | 400 | No | Quantity outside 1-99 range | `cart_test.go` |
| `currency_mismatch` | 400 | No | Item currency differs from cart | `cart_test.go` |
| `item_not_found` | 404 | No | Variant not in cart | `cart_test.go` |
| `missing_idempotency_key` | 400 | No | No Idempotency-Key header | `checkout_test.go` |
| `insufficient_stock` | 409 | Yes (restock) | Available stock < requested | `inventory_test.go`, `checkout_test.go` |
| `charge_failed` | 402 | Yes (retry payment) | Payment declined by provider | `payment_test.go`, `checkout_test.go` |
| `payment_timeout` | 504 | Yes | Payment provider timeout | `payment_test.go`, `checkout_test.go` |
| `internal_error` | 500 | No | Unexpected server error | N/A |

### Error Envelope Format

All HTTP errors respond with:

```json
{
  "error": {
    "code": "error_code_snake_case",
    "message": "Human-readable description",
    "request_id": "abc123def456"
  }
}
```

**Implementation**: `internal/platform/httpx.WriteError()` (httpx.go:35)

**Request ID**: Present in every response via `X-Request-ID` header. Generated if not provided.

## 7. API Conventions

### Pagination

- **Parameter**: `limit` (query string)
- **Default**: 20
- **Maximum**: 100 (enforced in `catalog/product.go:52-55`)
- **Cursor**: Opaque base64-encoded string (exact format TBD)
- **Response**:
  ```json
  {
    "data": [...],
    "next_cursor": "eyJpZCI6Imxhc3QifQ=="
  }
  ```
- **Behavior**: If `limit > 100`, returns `400 invalid_limit`

### Idempotency

- **Header**: `Idempotency-Key` (required on POST /v1/checkout)
- **Format**: Any non-empty string (user-provided)
- **TTL**: None (in-memory, cleared on restart) — **planned: 24h**
- **Behavior**:
  - Same key + same request → same response (cached)
  - Concurrent requests with same key → single processing (mutex)
- **Response**: Same order object on replay

### Versioning

- **URL prefix**: `/v1/`
- **Current version**: v1
- **Strategy**: URL-based versioning. Breaking changes → `/v2/`.

### Content Types

- **Request**: `Content-Type: application/json`
- **Response**: `Content-Type: application/json; charset=utf-8`
- **Error response**: Same format with error envelope

### 204 No Content

- **Endpoints**: `DELETE /v1/carts/{id}`, successful deletions
- **Response**: Empty body, 204 status code
- **Implementation**: `httpx.WriteJSON()` handles 204 (httpx.go:27-30)

## 8. Performance Budgets

### Latency Targets (p50/p99)

| Endpoint | p50 | p99 | Notes |
|----------|-----|-----|-------|
| `GET /v1/products` (catalog list) | < 50ms | < 200ms | In-memory (current). Target: < 100ms p99 Postgres with index |
| `GET /v1/products/{slug}` (product detail) | < 20ms | < 100ms | In-memory. Target: < 50ms p99 Postgres |
| `POST /v1/checkout` (place order) | < 500ms | < 2000ms | Depends on payment provider latency. Paystack: 200-500ms, M-Pesa: 1-30s |

**Current state**: No instrumentation. Targets are aspirational.

### N+1 Query Policy

**RULE**: Zero tolerance for N+1 queries.

- **Detection**: `staticcheck` S1000, code review
- **Prevention**: Eager loading, batching, or single queries
- **Example violation**:
  ```go
  // WRONG: N+1 - one query per product
  for _, productID := range productIDs {
      product := repo.Get(ctx, productID) // N queries
  }
  
  // CORRECT: Single query
  products := repo.GetByIDs(ctx, productIDs) // 1 query
  ```

**Current state**: Catalog uses in-memory repository (no N+1 risk). **Planned**: Postgres queries reviewed for N+1.

### Payload Size Caps

| Limit | Value | Enforcement |
|-------|-------|--------------|
| Request body | 1 MB (soft limit) | HTTP server `ReadTimeout` |
| Response JSON | 10 MB (planned) | Middleware check |
| Product list | 100 items max | `MaxLimit` constant (product.go:55) |
| Cart items | 99 items | `ErrInvalidQuantity` check |

**Hard limit**: Go HTTP server rejects requests > 1MB by default (configurable).

---

## Current Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| Single binary | ✅ Implemented | `cmd/kikapu/main.go` |
| In-memory cart store | ✅ Implemented | `internal/cart/store.go` |
| In-memory inventory | ✅ Implemented | `internal/inventory/inventory.go` |
| In-memory idempotency | ✅ Implemented | `internal/checkout/checkout.go:41-89` |
| Checkout flow | ✅ Implemented | Reserve → Charge → Save Order |
| Paystack provider | ✅ Implemented | `internal/payment/paystack/` |
| M-Pesa provider | ✅ Implemented | `internal/payment/mpesa/` |
| Webhook endpoints | ✅ Implemented | Not wired in routes (planned) |
| PostgreSQL | ❌ Not implemented | Phase 1 (planned) |
| Transaction support | ❌ Not implemented | Phase 3 (planned) |
| Event outbox | ❌ Not implemented | Phase 5 (planned) |
| Order management | ❌ Not implemented | Phase 2 (planned) |
| Promotion engine | ❌ Not implemented | Phase 4 (planned) |
