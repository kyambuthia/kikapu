# Roadmap

This document outlines the development phases from the current in-memory implementation to a production-ready PostgreSQL-backed commerce engine.

## Phase Overview

```
Phase 1: Foundation (DONE)
   ↓
Phase 2: Regions & Pricing
   ↓
Phase 3: Inventory & Orders
   ↓
Phase 4: Promotions & Tax
   ↓
Phase 5: Events & Webhooks
   ↓
Phase 6: Persistence & Admin
```

---

## Phase 1: Foundation ✅ COMPLETE

**Goal**: Single binary, in-memory stores, REST API, payment integration.

**Status**: Implemented

**Gate**:
- [x] Single binary builds and runs
- [x] Products API: list, get by slug, pagination
- [x] Cart API: create, add item, update, remove
- [x] Checkout API with idempotency
- [x] Payment providers: Fake, Paystack, M-Pesa
- [x] All tests passing
- [x] `make test` passes with ≥80% coverage on platform packages

**Medusa counterpart**: Product, Cart, Payment Provider modules

**Saleor counterpart**: Product, Checkout, Payment Gateway modules

**Depends on**: None

**Deliverables**:
- `internal/platform/money` — Money arithmetic in minor units
- `internal/platform/httpx` — Error envelopes, request ID
- `internal/catalog` — Product repository (in-memory)
- `internal/cart` — Cart store (in-memory)
- `internal/checkout` — Order placement with idempotency
- `internal/inventory` — Stock levels, reservations (in-memory)
- `internal/pricing` — Price resolution (not integrated)
- `internal/payment` — Provider interface, Fake implementation
- `internal/payment/paystack` — Paystack client, webhook handler
- `internal/payment/mpesa` — M-Pesa STK Push, callback handler

---

## Phase 2: Regions & Pricing

**Goal**: Multi-currency pricing, region-based price resolution, tax configuration.

**Status**: NOT STARTED

**Gate**:
- [ ] `GET /v1/regions` returns configured regions
- [ ] Products return prices in requested currency
- [ ] Cart respects region tax rules
- [ ] All tests passing

**Medusa counterpart**: Region, Currency, Price List

**Saleor counterpart**: Channel, Channel Listing, Tax Configuration

**Depends on**: Phase 1

**Tasks**:
1. Create `internal/region` package with Region model
2. Define `Region{ID, Currency, TaxRate, Countries}`
3. Implement region resolution by country/IP
4. Extend `Product` with multi-currency prices
5. Integrate `pricing.Resolve()` into cart total
6. Add tax calculation to cart (included or excluded)
7. Add routes: `GET /v1/regions`, `POST /v1/carts/{id}/region`

**Test coverage**: Region resolution, price calculation, tax rules

---

## Phase 3: Inventory & Orders

**Goal**: Persistent stock levels, order history, order management, transactional checkout.

**Status**: NOT STARTED

**Gate**:
- [ ] Stock levels persist across restarts
- [ ] `GET /v1/orders` lists orders with pagination
- [ ] `GET /v1/orders/{id}` returns order details
- [ ] Checkout is transactional (all-or-nothing)
- [ ] All tests passing

**Medusa counterpart**: Stock Location, Inventory Item, Order

**Saleor counterpart**: Warehouse, Stock, Order

**Depends on**: Phase 2

**Tasks**:
1. Add PostgreSQL with `internal/platform/postgres`
2. Create schema migrations: products, variants, orders, inventory
3. Implement `PostgresProductRepository`
4. Implement `PostgresOrderStore`
5. Implement `PostgresInventoryStore` with stock levels
6. Add transaction support (`sql.Tx`)
7. Wrap checkout in single transaction
8. Add order routes: `GET /v1/orders`, `GET /v1/orders/{id}`
9. Store order items, totals, status, timestamps

**Test coverage**: Order persistence, transaction recovery, stock consistency

---

## Phase 4: Promotions & Tax

**Goal**: Discount codes, percentage/fixed discounts, promotion rules, tax engine.

**Status**: NOT STARTED

**Gate**:
- [ ] Cart applies discount codes
- [ ] Percentage and fixed discounts supported
- [ ] Order reflects final price after discounts
- [ ] Tax calculated on discounted amount
- [ ] All tests passing

**Medusa counterpart**: Discount, Promotion Rule, Tax Provider

**Saleor counterpart**: Sale, Voucher, Tax Rate

**Depends on**: Phase 3

**Tasks**:
1. Create `internal/promotion` package
2. Define `Promotion{Code, Type, Value, Rules}` where Type ∈ {percentage, fixed}
3. Define `PromotionRule{Type, Value}` (min_amount, usage_limit, applies_to)
4. Implement `PromotionStore` interface
5. Add `PostgresPromotionStore`
6. Add discount calculation to cart total
7. Add routes:
   - `POST /v1/carts/{id}/promotions`
   - `DELETE /v1/carts/{id}/promotions/{code}`
8. Validate promotion rules before applying
9. Track promotion usage count
10. Integrate with tax calculation (discount before tax)

**Test coverage**: Code validation, discount calculation, rule enforcement, usage limits

---

## Phase 5: Events & Webhooks

**Goal**: Transactional outbox, domain events, webhook delivery, retry mechanism.

**Status**: NOT STARTED

**Gate**:
- [ ] Events published on: order.created, order.paid, payment.captured
- [ ] Events stored in outbox table (same transaction)
- [ ] Webhook delivery with signature
- [ ] Retry logic with exponential backoff
- [ ] All tests passing

**Medusa counterpart**: Event Bus, Subscriber, Webhook

**Saleor counterpart**: Webhook, Event, Subscription

**Depends on**: Phase 3

**Tasks**:
1. Create `internal/events` package
2. Define event types in event envelope format (see ARCHITECTURE.md)
3. Add `outbox` table to schema (id, type, payload, created_at, published_at)
4. Modify checkout to append events to outbox in same transaction
5. Implement worker mode to poll outbox and publish webhooks
6. Add webhook configuration: URL, secret, events filter
7. Add webhook signature (HMAC-SHA256)
8. Add retry with exponential backoff (max 5 retries)
9. Add dead-letter queue for failed webhooks
10. Document event schema in OpenAPI

**Test coverage**: Outbox atomicity, webhook signature, retry logic, deduplication

---

## Phase 6: Persistence & Admin

**Goal**: Replace all in-memory stores with PostgreSQL, add admin endpoints.

**Status**: NOT STARTED

**Gate**:
- [ ] All data persists in PostgreSQL
- [ ] Admin endpoints: CRUD for products, inventory, promotions
- [ ] Idempotency keys stored with TTL
- [ ] Dashboard can manage products, stock, orders
- [ ] All tests passing

**Medusa counterpart**: Admin API, Admin Dashboard

**Saleor counterpart**: Admin API, Dashboard

**Depends on**: Phase 5

**Tasks**:
1. Implement `PostgresCartStore` (replace in-memory)
2. Implement `PostgresIdempotencyStore` with TTL (24h default)
3. Add admin endpoints:
   - `POST /v1/admin/products` — Create product
   - `PATCH /v1/admin/products/{id}` — Update product
   - `DELETE /v1/admin/products/{id}` — Delete product
   - `POST /v1/admin/inventory` — Set stock level
   - `GET /v1/admin/orders` — List all orders
   - `PATCH /v1/admin/orders/{id}` — Update order status
4. Add authentication for admin endpoints (API key or JWT)
5. Add order fulfillment endpoints
6. Add refund endpoints (transition to `refunded` state)
7. Add metrics endpoint (Prometheus format)
8. Docker Compose setup for local development

**Test coverage**: Admin CRUD operations, auth, data persistence

---

## Definition of Done

Every phase must pass these gates:

### Code Quality

```bash
gofmt -l .         # No output (all files formatted)
goimports -l .     # No output (imports sorted)
go vet ./...       # Clean (no static analysis errors)
staticcheck ./...  # Clean
golangci-lint run  # Clean
go build ./...     # Success
go test -race ./... # All tests pass
```

### Coverage Requirements

| Package | Minimum Coverage |
|---------|------------------|
| `internal/platform/*` | 80% |
| `internal/cart` | 80% |
| `internal/checkout` | 80% |
| `internal/payment` | 70% |
| `internal/inventory` | 70% |
| `internal/catalog` | 70% |

Run: `go test -cover ./internal/platform/...`

### Documentation

- [ ] All endpoints documented in `openapi.yaml`
- [ ] All invariants documented and tested
- [ ] All modules have package-level godoc
- [ ] README links to relevant docs

### Commit Style

Conventional commits enforced:

- `feat(scope): add feature`
- `fix(scope): fix bug`
- `test(scope): add tests`
- `docs(scope): update docs`
- `chore(scope): maintenance`
- `refactor(scope): refactoring`

---

## Out of Scope

These features are explicitly **not** planned for the current roadmap:

1. **Multi-tenant** — Single store only
2. **User authentication** — No customer accounts (admin auth only)
3. **Shipping calculation** — No shipping zones or carriers
4. **Full-text search** — No Elasticsearch/Algolia integration
5. **Multi-currency FX** — Prices per currency, no conversion
6. **Product recommendations** — No ML engine
7. **Analytics dashboard** — No BI tools
8. **Mobile SDKs** — REST API only
9. **GraphQL API** — REST only
10. **Multi-language** — English only

---

## Dependency Graph

```
Phase 1: Foundation ✓
   ↓
Phase 2: Regions & Pricing
   ↓
Phase 3: Inventory & Orders
   ↓
Phase 4: Promotions & Tax
   ↓
Phase 5: Events & Webhooks
   ↓
Phase 6: Persistence & Admin
```

**Critical path**: Phase 1 → 3 → 5 → 6 (foundation, persistence, events, admin)

**Parallel work**: Phase 2 (pricing) and Phase 4 (promotions) can proceed independently after Phase 1.

---

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| Payment provider API changes | High | Interface abstraction in `payment.Provider` |
| Data loss on restart | High | Phase 3 persistence (PostgreSQL) |
| Idempotency key explosion | Medium | TTL enforcement (Phase 3) |
| N+1 queries in Postgres | High | Review all queries, use batch loading |
| Webhook delivery failures | Medium | Retry with exponential backoff (Phase 5) |
| Schema migration failures | High | Use tested migration tool, backup before migration |

---

## Timeline (Tentative)

| Phase | Duration | Dependencies |
|-------|----------|--------------|
| Phase 1: Foundation | ✅ Complete | None |
| Phase 2: Regions & Pricing | 2-3 weeks | Phase 1 |
| Phase 3: Inventory & Orders | 3-4 weeks | Phase 2 |
| Phase 4: Promotions & Tax | 2-3 weeks | Phase 3 |
| Phase 5: Events & Webhooks | 2-3 weeks | Phase 3 |
| Phase 6: Persistence & Admin | 3-4 weeks | Phase 5 |

**Total estimated duration**: 12-17 weeks

**Note**: Timeline depends on team size, availability, and scope changes.
