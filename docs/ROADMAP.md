# Roadmap

This document maps the path from the current in-memory implementation to a production-ready PostgreSQL-backed commerce engine.

## Module Mapping

| Kikapu Module | Medusa | Saleor | Notes |
|---------------|--------|--------|-------|
| `catalog` | Product, ProductVariant | Product, ProductVariant | Products and variants |
| `cart` | Cart | Checkout | Shopping cart |
| `checkout` | Order processing | Order processing | Order creation |
| `inventory` | StockLocation, InventoryItem | Warehouse, Stock | Stock levels |
| `pricing` | Region (partial) | Channel | Price per channel/region |
| `payment` | PaymentProvider | PaymentGateway | Payment abstraction |
| `payment/paystack` | Paystack plugin | - | Payment gateway |
| `payment/mpesa` | - | - | Mobile money |
| (not yet) `order` | Order | Order | Order management |
| (not yet) `promotion` | Discount, PromotionRule | Sale, Voucher | Discounts |
| (not yet) `events` | EventBus | Webhooks | Domain events |

## Phases

### Phase 1: PostgreSQL Persistence

**Why**: In-memory stores lose all state on restart. PostgreSQL is the source of truth.

**Depends on**: Nothing (foundation)

**Gate**:
- Docker Compose with Postgres
- `internal/platform/postgres` package with connection pool
- Schema migrations using `golang-migrate`
- All tests pass with `make test`
- `go build ./...` succeeds

**Tasks**:
1. Create `internal/platform/postgres` with connection pool and health check
2. Create `migrations/` directory with initial schema (products, carts, orders, inventory)
3. Implement `PostgresCartStore` behind `cart.Store` interface
4. Implement `PostgresProductRepository` behind `catalog.Repository` interface
5. Add `KIKAPU_DATABASE_URL` config

---

### Phase 2: Order Management

**Why**: Orders are created but cannot be retrieved or managed. No order history.

**Depends on**: Phase 1 (Postgres)

**Gate**:
- `GET /v1/orders/{id}` returns order
- `GET /v1/orders` lists orders with pagination
- Orders persist across restarts
- All tests pass

**Tasks**:
1. Create `internal/order` package with `Order` domain model
2. Define `OrderStore` interface
3. Implement `PostgresOrderStore`
4. Add order routes: `GET /v1/orders`, `GET /v1/orders/{id}`
5. Wire order store into checkout service
6. Add order total, items, status in schema

---

### Phase 3: Transaction Safety

**Why**: Checkout reserves inventory, charges payment, and saves order. Must be atomic.

**Depends on**: Phase 2 (Order management)

**Gate**:
- Failed payment releases reservations
- Partial failures leave no orphaned state
- Idempotency keys persist across restarts
- All tests pass

**Tasks**:
1. Add transaction support to Postgres layer
2. Implement `PostgresIdempotencyStore` with TTL
3. Checkout service uses single transaction for: reserve inventory, charge payment, save order
4. Add rollback for payment failures
5. Add idempotency TTL config (`KIKAPU_IDEMPOTENCY_TTL`, default 24h)

---

### Phase 4: Promotion Engine

**Why**: Discounts, coupons, and promotions are essential for commerce.

**Depends on**: Phase 3 (Transaction safety)

**Gate**:
- Cart applies percentage and fixed discounts
- Discount codes validated before applying
- Order reflects final price after discounts
- All tests pass

**Tasks**:
1. Create `internal/promotion` package
2. Define `Promotion` model (code, type, amount, rules)
3. Define `PromotionStore` interface
4. Implement `PostgresPromotionStore`
5. Add discount calculation to cart
6. Add routes: `POST /v1/carts/{id}/promotions`, `DELETE /v1/carts/{id}/promotions/{code}`
7. Calculate tax on discounted price

---

### Phase 5: Event Publishing

**Why**: External systems need to react to domain events (inventory sync, analytics, fulfillment).

**Depends on**: Phase 3 (Transaction safety)

**Gate**:
- Events published on order creation, payment capture
- Events stored in outbox table (same transaction)
- Event schema documented
- All tests pass

**Tasks**:
1. Create `internal/events` package
2. Define event types: `order.created`, `order.paid`, `payment.captured`, `inventory.reserved`
3. Add `outbox` table to schema
4. Service appends events to outbox in same transaction
5. Worker mode reads from outbox, publishes to webhook/queue
6. Document event envelope schema in OpenAPI

## Definition of Done

Every phase and task must pass:

```bash
gofmt -l .         # No output
goimports -l .     # No output
go vet ./...       # Clean
staticcheck ./...  # Clean
golangci-lint run  # Clean
go build ./...     # Success
go test -race ./... # All pass
```

Coverage requirements:
- `internal/platform/*`: ≥ 80%
- `internal/cart`: ≥ 80%
- `internal/checkout`: ≥ 80%
- `internal/payment`: ≥ 70%

Commit style: Conventional commits (`feat:`, `fix:`, `test:`, `docs:`, `chore:`)

## Not Yet

These are explicitly out of scope for the current roadmap:

1. **PostgreSQL** — Phase 1 (just documentation for now)
2. **Admin API** — No admin dashboard, no management endpoints
3. **Search** — No Elasticsearch, Algolia, or full-text search
4. **Multi-currency FX** — Prices are per currency, no conversion
5. **Customer accounts** — No user authentication, no customer model
6. **Shipping** — No shipping zones, methods, or calculation
7. **Tax calculation** — No tax engine, tax rates, or VAT handling
8. **Refunds** — Payment has `refunded` state but no refund endpoints
9. **Metrics** — No Prometheus or OpenTelemetry metrics
10. **Rate limiting** — No request throttling
