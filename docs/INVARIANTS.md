# Invariants

This document defines the architectural invariants enforced by kikapu. Each invariant is numbered, testable, and references its enforcement mechanism.

## Money Invariants

### INV-1: Money is stored and computed in minor units; no float in any money path.

**Enforcement**:
- Type: `internal/platform/money.Amount{Value int64, Currency string}` (money.go:16-19)
- All arithmetic returns `int64` values with overflow checks
- **Test coverage**: `internal/platform/money/money_test.go` — 8 test functions covering parse, format, add, sub, mul, div, rounding

**Rationale**: Floats cannot represent 0.10 exactly in binary. Minor units (cents) avoid rounding errors.

**Checkable**: `grep -r "float64.*money" internal/` returns nothing.

---

### INV-2: Every mutation endpoint requires Idempotency-Key; replay returns the original result.

**Enforcement**:
- Endpoint: `POST /v1/checkout` (routes/cart.go:239-242)
- Missing key: Returns 400 `missing_idempotency_key`
- Storage: `checkout.IdempotencyStore` (checkout.go:41-89)
- Replay behavior: Returns same order (checkout.go:130-132)
- Concurrent safety: `sync.RWMutex` guards keys map

**Test coverage**: 
- `internal/checkout/checkout_test.go:TestService_PlaceOrder_DoubleSubmit` — verifies replay returns same order
- `internal/checkout/checkout_test.go:TestService_PlaceOrder_Concurrent` — verifies single order creation under concurrent access

**Current limitations**:
- Idempotency keys stored in-memory (cleared on restart)
- No TTL (keys persist indefinitely)
- **Planned**: Persistent store with 24h TTL in Phase 3

---

## Inventory Invariants

### INV-3: Stock is never negative; Available = OnHand - Reserved ≥ 0 at all times.

**Enforcement**:
- Code: `internal/inventory/inventory.go:68-82` — Reserve() checks before decrementing
- Formula: `if available-reserved < quantity { return ErrInsufficientStock }` (line 80-81)
- Mutex: `sync.RWMutex` ensures atomic check-and-reserve

**Test coverage**: 
- `internal/inventory/inventory_test.go` — tests insufficient stock scenarios
- `internal/checkout/checkout_test.go:TestService_PlaceOrder_ExpiredReservation` — verifies stock invariant

**Current limitations**:
- In-memory only; no persistent stock levels
- **Planned**: PostgreSQL constraint `CHECK (available >= 0)` in Phase 1

**Checkable**: No code path decrements stock below zero.

---

## Event Invariants

### INV-4: An event exists iff the state change committed (outbox atomicity).

**Enforcement**: 
- **Current state**: NOT IMPLEMENTED
- No event store, no outbox pattern
- Checkout creates orders in-memory without events

**Test coverage**: None (feature not implemented)

**Planned**: 
- Phase 5 — Event Publishing
- Outbox table in same transaction as order creation
- Worker mode publishes from outbox
- Test: Event exists => order committed

---

## Order Invariants

### INV-5: Order totals are immutable after capture.

**Enforcement**:
- Code: `internal/checkout/checkout.go:174-183` — Order struct has Total field set once
- No update method on Order
- Cart cleared after order creation (line 189)

**Test coverage**: 
- No explicit test for immutability
- **Missing test**: Verify order total cannot be modified

**Current limitations**:
- No database, no enforcement beyond runtime
- **Planned**: PostgreSQL immutable order_line table

---

## Webhook Invariants

### INV-6: Webhook deliveries are signed and replayable-safe by delivery id.

**Enforcement**:
- **Paystack** (internal/payment/paystack/webhook.go):
  - Signature: HMAC-SHA512 (line 37-47)
  - Idempotency: `InMemoryEventStore` by event_id + reference (line 68-88)
  - Test: `paystack/webhook_test.go` — verifies signature and deduplication
  
- **M-Pesa** (internal/payment/mpesa/callback.go):
  - Signature: NONE (IP allowlist only, line 42-53)
  - Idempotency: `InMemoryCallbackStore` by CheckoutRequestID + ResultCode (line 88-110)
  - Test: `mpesa/callback_test.go` — verifies deduplication

**Test coverage**: 
- `internal/payment/paystack/webhook_test.go` — TestWebhookHandler_HandleSuccess
- `internal/payment/mpesa/callback_test.go` — TestCallbackHandler_HandleCallback

**Current limitations**:
- M-Pesa has NO signature verification (only IP allowlist)
- Stores are in-memory, cleared on restart
- **Planned**: Persistent event store with TTL in Phase 3

---

## Module Boundary Invariants

### INV-7: No package reads another module's tables/structs directly; interfaces at the consumer.

**Enforcement**:
- Dependency graph enforced by code review and architecture tests
- Example: `checkout.Service` depends on `cart.Store` interface, not `cart.InMemoryStore`
- Example: `routes.CartHandler` depends on `checkout.Service`, not `checkout.IdempotencyStore`

**Checkable** (should be automated):
```bash
# Cart should never import checkout
grep -r "import.*checkout" internal/cart/ # should return nothing

# Catalog should never import order (doesn't exist yet)
grep -r "import.*order" internal/catalog/ # should return nothing

# Routes should never import payment implementations
grep -r "import.*payment/paystack" internal/routes/ # should return nothing
grep -r "import.*payment/mpesa" internal/routes/ # should return nothing
```

**Test coverage**: No automated test. Manual code review.

**Planned**: Automated architecture tests using `go/tools/go/packages` or architecture testing framework.

---

## Security Invariants

### INV-8: Every response carries X-Request-ID; logs never contain PAN, tokens, or phone numbers.

**Enforcement**:
- Request ID: `internal/platform/httpx.RequestIDMiddleware` (httpx.go:76-87)
  - Reads from `X-Request-ID` header or generates new ID
  - Sets in response header and context
  - Logs include request_id (httpx.go:102)
  
- Sensitive data filtering:
  - **Current state**: No explicit filtering
  - **Missing**: No test verifying PAN/tokens are scrubbed from logs
  - **Risk**: M-Pesa phone numbers logged (callback payload)

**Test coverage**: 
- Request ID: `internal/platform/httpx/httpx_test.go` — tests generation and propagation
- Log scrubbing: NO TEST

**Current limitations**:
- No log scrubbing implementation
- M-Pesa callback logs phone numbers (line in mpesa/callback.go)
- **Action needed**: Implement log scrubbing for phone numbers, PANs, tokens

---

## Summary

| Invariant | Enforced | Test Coverage | Critical |
|-----------|----------|---------------|----------|
| INV-1: Money in minor units | ✅ Yes | ✅ Comprehensive | Yes |
| INV-2: Idempotency on mutations | ✅ Yes | ✅ Yes | Yes |
| INV-3: Stock non-negative | ✅ Yes | ✅ Yes | Yes |
| INV-4: Event outbox atomicity | ❌ No | ❌ Not implemented | Medium |
| INV-5: Order total immutable | ⚠️ Runtime only | ⚠️ Missing test | Medium |
| INV-6: Webhook idempotency | ✅ Yes | ✅ Yes | Yes |
| INV-7: Module boundaries | ⚠️ Manual review | ❌ No automated test | Medium |
| INV-8: No secrets in logs | ⚠️ Partial | ❌ No test | High |

**Priority fixes**:
1. INV-8: Implement log scrubbing for phone numbers/PANs/tokens
2. INV-7: Add automated architecture tests
3. INV-5: Add test for order total immutability
4. INV-4: Implement event outbox in Phase 5
