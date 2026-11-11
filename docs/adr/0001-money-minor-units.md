# ADR-0001: Money in Minor Units

## Status

Accepted

## Context

Financial calculations require exact decimal representation. Standard floating-point types (`float32`, `float64`) cannot exactly represent values like 0.10, leading to rounding errors that accumulate over multiple operations.

Example:
```go
// WRONG: Typical float-based money
price := 19.99 * 3  // 59.96999999999999 (not exactly 59.97)
```

Existing alternatives:
1. Use `float64` with rounding (risky, error-prone)
2. Use a decimal library (e.g., `github.com/shopspring/decimal`)
3. Store money as integers in minor units (cents, pence, satoshis)

## Decision

We store all monetary values as `int64` in minor units.

Example:
```go
// CORRECT: Integer minor units
type Amount struct {
    Value    int64   // 2999 means $29.99
    Currency string  // "USD"
}

// $29.99 × 3 = $89.97
price := Amount{Value: 2999, Currency: "USD"}
total, _ := price.Mul(3)  // total.Value = 8997
```

### Implementation Details

- Package: `internal/platform/money`
- Type: `Amount{Value int64, Currency string}`
- Operations: Add, Sub, Mul, Div with overflow checks
- Rounding: Explicit modes (RoundHalfEven, RoundHalfUp, RoundDown)
- Errors: `ErrOverflow`, `ErrCurrencyMismatch`, `ErrDivisionByZero`

### Why Basis Points?

We use 100 basis points per currency unit (cents, pence) because:
1. Most currencies have 2 decimal places (USD, EUR, GBP, KES)
2. 100 fits in `int64` with room for large values (up to ~92 quadrillion dollars)
3. Simple mental model: divide by 100 to get human-readable amount

For currencies without decimals (JPY), multiply by 1.

For cryptocurrencies (satoshi), use 100,000,000 basis points.

## Consequences

### Positive

- **Exact arithmetic**: No floating-point rounding errors
- **Overflow safety**: `int64` overflow returns explicit error
- **Simple**: No external dependencies
- **Fast**: Integer arithmetic is faster than decimal libraries
- **Portable**: Works identically across all platforms

### Negative

- **Division requires rounding**: Must choose explicit rounding mode
- **Presentation layer**: Must divide by 100 to display to users
- **Currency-specific logic**: Some currencies have different decimal places (JPY has none)

### Rejected Alternatives

#### Alternative 1: `float64` with rounding

```go
type Amount float64 // WRONG
```

**Why rejected**: Floating-point representation is inexact. Accumulated errors over many operations cause incorrect totals. No compile-time enforcement of rounding.

#### Alternative 2: Decimal library

```go
import "github.com/shopspring/decimal"
type Amount struct {
    Value decimal.Decimal
}
```

**Why rejected**: External dependency for simple arithmetic. Slower than integer operations. Overkill for money (no need for arbitrary precision). Adds complexity to database schema and serialization.

## Examples

### Addition

```go
a := Amount{Value: 1999, Currency: "USD"} // $19.99
b := Amount{Value: 500, Currency: "USD"}  // $5.00
total, _ := a.Add(b) // total.Value = 2499 ($24.99)
```

### Currency Mismatch

```go
a := Amount{Value: 1999, Currency: "USD"}
b := Amount{Value: 1500, Currency: "EUR"}
_, err := a.Add(b) // err == ErrCurrencyMismatch
```

### Overflow Protection

```go
a := Amount{Value: math.MaxInt64 - 10, Currency: "USD"}
b := Amount{Value: 100, Currency: "USD"}
_, err := a.Add(b) // err == ErrOverflow
```

### Division with Rounding

```go
a := Amount{Value: 1000, Currency: "USD"}
b, _ := a.Div(3)              // b.Value = 333 ($3.33, truncated)
c, _ := a.RoundHalfEven(1)    // c.Value = 1000 (already exact)
```

### Line Total

```go
unitPrice := Amount{Value: 2999, Currency: "USD"} // $29.99
quantity := 3
total, _ := unitPrice.Mul(int64(quantity)) // total.Value = 8997 ($89.97)
```

## Enforcement

- **Type**: Compile-time enforcement via `money.Amount` struct
- **Tests**: `internal/platform/money/money_test.go` — 8 test functions
- **Lint**: Static analysis ensures no `float64` in money paths

## References

- [What Every Computer Scientist Should Know About Floating-Point Arithmetic](https://docs.oracle.com/cd/E19957-01/806-3568/ncg_goldberg.html)
- [Martin Fowler: Money Pattern](https://martinfowler.com/eaaCatalog/money.html)
- [Stripe API: Currency amounts are integers](https://stripe.com/docs/currencies)
