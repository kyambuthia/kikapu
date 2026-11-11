# Invariants

Eight numbered, testable rules that define kikapu's correctness boundaries. Each invariant names the test file that enforces it, or states `enforcement: missing`.

---

## INV-1: Money in Minor Units

**Statement**: Money is stored and computed in minor units (cents, pence, satoshis); no `float64` in any money path.

**Enforcement**: `internal/platform/money/money_test.go`

**How to check**:
- `go test ./internal/platform/money/...` (8+ test functions covering parse, add, sub, mul, div, rounding)
- `grep -r "float64" internal/platform/money/` returns nothing

---

## INV-2: Idempotent Mutations

**Statement**: Every mutation endpoint requires `Idempotency-Key`; replay with the same key returns the original response.

**Enforcement**: `internal/checkout/checkout_test.go` (tests: `TestService_PlaceOrder_DoubleSubmit`, `TestService_PlaceOrder_Concurrent`)

**How to check**:
- `go test -run 'DoubleSubmit|Concurrent|Idempoten' ./internal/checkout/... ./internal/routes/...`
- Payment provider called exactly once for concurrent requests with same key

---

## INV-3: Stock Never Negative

**Statement**: Stock is never negative; `Available = OnHand - Reserved >= 0`.

**Enforcement**: `internal/inventory/inventory_test.go` (test: `TestService_Reserve` with "insufficient stock" case)

**How to check**:
- `go test -run 'Reserve|Expired' ./internal/inventory/...`
- `ErrInsufficientStock` returned when `quantity > available`

---

## INV-4: Event-State Atomicity

**Statement**: An event exists iff the state change committed (outbox atomicity).

**Enforcement**: missing (outbox not implemented - planned Phase 5)

**How to check**:
- Review ROADMAP.md Phase 5 for planned implementation
- Once implemented: event store and domain state in same transaction

---

## INV-5: Order Totals Immutable

**Statement**: Order totals are immutable after capture.

**Enforcement**: `internal/checkout/checkout_test.go` (implicit in order creation flow)

**How to check**:
- Orders are created with final totals (`checkout.go:174-183`)
- No `UpdateOrder` mutation exists on order totals
- `grep -r "Order.Total =" internal/` should only show creation sites

---

## INV-6: Webhook Security

**Statement**: Webhook deliveries are signed and replay-safe by delivery id.

**Enforcement**: `internal/payment/paystack/paystack_test.go` (tests: `TestWebhookSignature`, `TestWebhookIdempotency`)

**How to check**:
- `go test -run 'WebhookSignature|WebhookIdempotency' ./internal/payment/paystack/...`
- HMAC-SHA512 signature validation
- Event deduplication by event ID

---

## INV-7: Module Boundary Enforcement

**Statement**: No package reads another module's tables or structs directly.

**Enforcement**: missing (review rule, enforced by code review)

**How to check**:
- Review ARCHITECTURE.md Section 3 (Module Map) for ownership table
- `grep -r "internal/cart" internal/checkout/` should fail (checkout uses cart.Store interface, not internal structs)
- Architectural imports validated in `Module Map` table

---

## INV-8: Request ID and Logging Hygiene

**Statement**: Every response carries `X-Request-ID`; logs never contain PAN, tokens, or phones.

**Enforcement**: `internal/platform/httpx/httpx_test.go` (tests: `TestRequestIDMiddleware`, `TestRequestID`)

**How to check**:
- `go test -run 'RequestID' ./internal/platform/httpx/...`
- Response headers include `X-Request-ID`
- `grep -rE "(PAN|phone|token)" internal/platform/obs/` returns nothing sensitive

---

## Summary Table

| INV | Statement | Enforcement |
|-----|-----------|-------------|
| INV-1 | Money in minor units, no float | `money/money_test.go` |
| INV-2 | Idempotent mutations | `checkout_test.go` |
| INV-3 | Stock never negative | `inventory_test.go` |
| INV-4 | Event-state atomicity | missing (planned) |
| INV-5 | Order totals immutable | `checkout_test.go` (implicit) |
| INV-6 | Webhook signed & replay-safe | `paystack_test.go` |
| INV-7 | Module boundary | missing (review rule) |
| INV-8 | Request ID & logging hygiene | `httpx_test.go` |

---

## Running All Checks

```bash
# Run all invariant tests
go test ./internal/platform/money/... ./internal/checkout/... ./internal/inventory/... \
        ./internal/platform/httpx/... ./internal/payment/paystack/... \
        -v -count=1

# Verify no float in money
grep -r "float64" internal/platform/money/ && echo "FAIL: float found" || echo "OK"

# Verify Request ID in responses
go test -run 'RequestID' ./internal/platform/httpx/...
```
