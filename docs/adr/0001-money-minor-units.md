# ADR-0001: Money in Minor Units

Status: accepted
Date: 2026-11-11

## Context

Financial applications require precise money calculations. Floating-point arithmetic introduces rounding errors that can accumulate over time and cause financial discrepancies. For example, `0.1 + 0.2` in floating-point does not equal `0.3` exactly.

Commerce systems deal with:
- Cart totals (sum of line items)
- Tax calculations (percentage of subtotal)
- Payment processing (gateway requires exact amount)
- Refunds (must match original charge)

## Decision

All monetary values are stored and computed as `int64` in minor units (cents, pence, satoshis, etc.). The `internal/platform/money` package provides:
- `Amount{Value int64, Currency string}` type
- Arithmetic: `Add`, `Sub`, `Mul`, `Div` with overflow checks
- Rounding: `RoundHalfEven` (banker's rounding), `RoundHalfUp`, `RoundDown`
- Currency mismatch detection

Example: `$29.99 USD` → `Amount{Value: 2999, Currency: "USD"}`

## Consequences

**Positive**:
- Exact arithmetic, no floating-point errors
- Overflow checks prevent silent corruption
- Simple serialization (JSON, database)
- Matches payment gateway APIs (Paystack, M-Pesa use integers)

**Negative**:
- Requires display formatting (cents to dollars)
- Division requires explicit rounding
- Not suitable for fractional units (e.g., cryptocurrency satoshis need bigger int)

**Rejected**:
- `decimal` library: Adds external dependency, slower than int64
- `float64`: Rounding errors unacceptable for money

## References

- `internal/platform/money/money.go`
- `internal/platform/money/money_test.go` (8+ test cases)
- INV-1 in `docs/INVARIANTS.md`
