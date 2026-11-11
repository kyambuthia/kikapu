# ADR-0004: Interface at Consumer

Status: accepted
Date: 2026-11-11

## Context

Go's dependency injection philosophy: "accept interfaces, return structs". The question: where should interfaces be defined?

Two approaches:
1. **Interface at provider**: Define interface near implementation
2. **Interface at consumer**: Define interface in package that uses it

Example: `checkout.Service` depends on `cart.Store`, `inventory.Service`, `payment.Provider`.

## Decision

Define interfaces at the consumer (caller) side.

Rule:
- **Consumer defines interface**: `internal/checkout` defines what it needs
- **Provider implements interface**: `internal/cart` implements, but doesn't define

Example:
```go
// internal/checkout/checkout.go
type CartStore interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}

// internal/cart/store.go
type InMemoryStore struct { ... }
func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }
```

Checkout **imports** from cart for the struct type, but the **interface** lives in checkout.

## Consequences

**Positive**:
- Consumer declares exactly what it needs (minimal interface)
- Provider can change without affecting consumer if interface stable
- Test doubles are trivial to create
- No import cycles

**Negative**:
- Interface duplication if multiple consumers need same interface
- May define same interface in multiple packages

**Mitigation**: If 3+ consumers need same interface, move to shared location (e.g., `internal/platform/`).

**Rejected**:
- **Interface at provider**: Creates coupling, consumer imports whole package for interface
- **IoC container**: Over-engineering for Go

## References

- `internal/checkout/checkout.go` (defines `cart.Store` usage via interface returned from `cart.NewInMemoryStore()`)
- ARCHITECTURE.md Section 3 (Module Map ownership)
- https://go.dev/blog/wire (Go team recommendation)
