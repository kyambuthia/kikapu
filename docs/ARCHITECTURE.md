# Architecture

This document describes the current architecture of kikapu. All claims reflect the implemented codebase, not future plans.

## Module Map

| Package | Owns | Must Not Touch |
|---------|------|---------------|
| `internal/platform/config` | Environment variables, defaults, validation | Business logic |
| `internal/platform/httpx` | Error envelopes, JSON responses, request ID middleware | Domain types |
| `internal/platform/money` | Amount type, arithmetic (add/sub/mul/div), rounding, overflow checks | Floats, business logic |
| `internal/platform/obs` | Structured JSON logging | Request processing |
| `internal/catalog` | Product model, variant model, repository interface, pagination | Orders, payments, cart |
| `internal/cart` | Cart model, line model, total calculation, store interface | Payment, checkout orchestration |
| `internal/checkout` | Order placement, idempotency, inventory reservation flow | HTTP handling |
| `internal/payment` | Provider interface, payment state machine, charge flow | HTTP routes |
| `internal/payment/paystack` | Paystack API client, webhook verification | M-Pesa, cart |
| `internal/payment/mpesa` | M-Pesa STK Push, callback handling, IP allowlist | Paystack, cart |
| `internal/inventory` | Stock levels, reservation TTL, clock abstraction | Orders, payments |
| `internal/pricing` | Channel-based price resolution | Cart mutation, payment |
| `internal/routes` | HTTP handlers, request parsing, response serialization | Database access, business logic |

## Dependency Rule

**Principle**: Dependencies flow inward and downward. Platform → Domain → Routes. Domain packages depend only on platform utilities or their own interfaces.

```
OK:    checkout → cart (via Store interface)
OK:    checkout → inventory (via Service interface)
OK:    checkout → payment (via Provider interface)
OK:    payment/paystack → payment (implements Provider)
OK:    cart → money (platform utility)

WRONG: catalog → order (catalog must not know orders exist)
WRONG: routes → db (handlers call services, never SQL)
WRONG: routes → payment/paystack (routes depends on payment abstraction, not specific provider)
WRONG: cart → checkout (cart must not know checkout exists)
WRONG: inventory → order (inventory must not know about orders)
```

## Request Flow

```
HTTP → routes (handlers) → service (domain logic) → repository/store (interface)
                                  ↓
                            events.Append (webhook idempotency store)
```

**Concrete flow for checkout**:

```
POST /v1/checkout
  → RequestIDMiddleware
  → LoggingMiddleware
  → CartHandler.Checkout
  → checkout.Service.PlaceOrder
      → IdempotencyStore.Get (check existing)
      → cart.Store.Get (load cart)
      → inventory.Service.Reserve (multiple items)
      → payment.Provider.Charge
      → (on failure) inventory.Service.Release
      → IdempotencyStore.SaveOrder
      → cart.Store.Save (clear cart)
```

**Handler responsibility**: Parse request, call service, write response. No business logic.

**Service responsibility**: Orchestrate domain operations, enforce invariants. No HTTP.

**Repository/Store responsibility**: Persist and retrieve domain objects. No business logic.

## Money Rules

1. All monetary values are stored as `int64` in minor units (cents, pence, satoshis)
2. No `float64` anywhere in the money path
3. All operations that can overflow return errors
4. Division requires explicit rounding mode: `RoundHalfEven`, `RoundHalfUp`, `RoundDown`
5. Currency comparison on add/subtract; mismatched currencies return `ErrCurrencyMismatch`
6. Multiplication for line totals: `unitPrice.Mul(quantity)`
7. Money package is the only way to handle currency amounts

```go
price := money.Amount{Value: 2999, Currency: "USD"}  // $29.99
total, err := price.Mul(3)                            // $89.97
formatted := total.Format()                           // "89.99 USD"
```

## Idempotency Rules

### Checkout Endpoint

- **Required**: `Idempotency-Key` header on `POST /v1/checkout`
- **Missing key**: Returns 400 with `missing_idempotency_key` error
- **Storage**: In-memory `IdempotencyStore` with `keys` and `orders` maps
- **Behavior**:
  - First request: processes checkout, stores order under that key
  - Duplicate request: returns existing order without re-processing
  - Concurrent requests: mutex ensures single order creation
- **TTL**: Currently none (stored in memory, lost on restart)

### Payment Webhooks

- **Paystack**: `InMemoryEventStore` tracks by `event_id` + `reference`
- **M-Pesa**: `InMemoryCallbackStore` tracks by `CheckoutRequestID` + `ResultCode`
- **Duplicate receipt**: 200 OK, logged, no state change

## Event Envelope

Webhooks and future event store use this envelope format:

```json
{
  "id": "evt_1705315200",
  "type": "payment.captured",
  "version": 1,
  "occurred_at": "2026-11-11T09:00:00Z",
  "data": {
    "payment_id": "pay_abc123",
    "amount": {"value": 2999, "currency": "USD"},
    "reference": "txn_xyz789"
  }
}
```

**Fields**:
- `id`: Unique event identifier (string)
- `type`: Domain event type in `{entity}.{action}` format
- `version`: Schema version (integer, starting at 1)
- `occurred_at`: ISO 8601 timestamp when the event occurred
- `data`: Event-specific payload

## Current Limitations

These are **current limitations**, not design decisions:

1. **In-memory storage**: All stores (cart, idempotency, events) are in-memory. State is lost on restart.
2. **Idempotency TTL**: No expiration; keys persist until restart
3. **No transaction support**: Checkout operations cannot be rolled back atomically
4. **Interface parameter types**: Repository uses `ctx interface{}` instead of `context.Context`
5. **Webhooks not registered**: Paystack and M-Pesa webhook handlers exist but not wired in `main.go`
6. **Pricing not integrated**: `pricing.Resolve()` exists but not called from cart/checkout
