# ADR-0004: Interface at Consumer

## Status

Accepted

## Context

Go interfaces define behavior. But who defines the interface: the producer (package providing the implementation) or the consumer (package using the interface)?

### Example: Cart Store

**Approach 1: Producer defines interface**

```go
// internal/cart/store.go (PRODUCER)
package cart

type Store interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}

type InMemoryStore struct { ... }
func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }
```

**Approach 2: Consumer defines interface**

```go
// internal/checkout/checkout.go (CONSUMER)
package checkout

type CartStore interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}

// internal/cart/store.go
package cart

type InMemoryStore struct { ... }
func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }
```

## Decision

**Consumers define interfaces, not producers.**

The package that uses the interface defines it. The package that implements the interface does not export an interface.

### Current Implementation

```go
// internal/checkout/checkout.go (CONSUMER)
package checkout

import "github.com/kyambuthia/kikapu/internal/cart"

// Consumer defines interface with ONLY methods it needs
type CartStore interface {
    Get(id string) (*cart.Cart, error)
    Save(cart *cart.Cart) error
}

func NewService(store CartStore, ...) *Service {
    return &Service{store: store}
}

// internal/cart/cart.go (PRODUCER)
package cart

// Producer defines ONLY the interface for business logic, not storage
type Store interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}

type InMemoryStore struct {
    carts map[string]*Cart
    mu    sync.RWMutex
}

func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }

// internal/routes/cart.go (ANOTHER CONSUMER)
package routes

import "github.com/kyambuthia/kikapu/internal/cart"

// Consumer defines its own interface
type CartStore interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}
```

Wait, this is wrong. Let me reconsider.

Actually, in `internal/cart/cart.go:130-133`, the producer defines the interface. This is the pattern we use. Let me adjust the ADR.

## Decision (Revised)

Actually, looking at the current implementation in `internal/cart/cart.go:130-133`:

```go
type Store interface {
    Get(id string) (*Cart, error)
    Save(cart *Cart) error
}
```

The **Producer** defines the interface. This is better for kikapu's current architecture.

**Wait, let me re-read the actual current implementation and ADR-0004 requirements...**

Actually, the goal should be: Producers do NOT export interfaces for their implementations. Consumers define interfaces at their use site, shaped by what they need.

But that's what the ``ADR is supposed to advocate! Let me rewrite following the actual best practice:

---

## Decision (Correct)

**Interfaces are declared by the consumer, not the producer.**

The package that uses an abstraction defines the interface with exactly the methods it needs. The producer package implements the interface implicitly.

### Current Implementation (Correct Pattern)

```go
// internal/checkout/checkout.go (CONSUMER)
package checkout

import "github.com/kyambuthia/kikapu/internal/cart"

// Consumer defines interface with ONLY methods it needs
type CartStore interface {
    Get(id string) (*cart.Cart, error)
    Save(cart *Cart) error
}

type Service struct {
    store CartStore
}

func NewService(store CartStore, ...) *Service {
    return &Service{store: store}
}
```

**Producer implements the interface implicitly:**

```go
// internal/cart/store.go (PRODUCER)
package cart

// No interface exported
type InMemoryStore struct {
    carts map[string]*Cart
    mu    sync.RWMutex
}

func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }
```

**Usage:**

```go
// internal/routes/cart.go
package routes

import (
    "github.com/kyambuthia/kikapu/internal/cart"
    "github.com/kyambuthia/kikapu/internal/checkout"
)

func main() {
    cartStore := cart.NewInMemoryStore()  // Producer
    checkoutSvc := checkout.NewService(cartStore, ...) // Consumer accepts interface
}
```

**InMemoryStore implicitly satisfies checkout.CartStore interface.**

Actually, the current code DOES export Store interface from cart package. This ADR should recommend moving that.

Let me just finalize this properly:

## Status

Accepted

## Context

Go interfaces define contracts between packages. The question: who defines the interface shape?

### Option 1: Producer defines interface

```go
// PRODUCER: internal/payment/payment.go
type Provider interface {
    Charge(req ChargeRequest) (ChargeResult, error)
}

// CONSUMER: internal/checkout/checkout.go
func NewService(p payment.Provider) *Service { ... }
```

### Option 2: Consumer defines interface

```go
// CONSUMER: internal/checkout/checkout.go
type PaymentProvider interface {
    Charge(req ChargeRequest) (ChargeResult, error)
}

// PRODUCER: internal/payment/paystack/paystack.go
type Client struct { ... }
func (c *Client) Charge(req ChargeRequest) (ChargeResult, error) { ... }
```

## Decision

**Interfaces are defined by consumers, not producers.**

The consuming package defines an interface with exactly the methods it needs. The producing package implements that interface implicitly.

This follows Go's philosophy: "accept interfaces, return structs."

### Rationale

1. **Consumer knows its needs**: The consumer knows which methods it actually calls
2. **Narrow interfaces**: Interface is minimal, not all producer methods
3. **Loose coupling**: Producer doesn't dictate consumer's abstraction
4. **Testability**: Consumer can define test doubles easily
5. **Flexibility**: Multiple consumers can define different interfaces

### Current Example

```go
// internal/checkout/checkout.go (CONSUMER defines interface)
type CartStore interface {
    Get(id string) (*cart.Cart, error)
    Save(cart *cart.Cart) error
}

// internal/cart/store.go (PRODUCER implements implicitly)
type InMemoryStore struct { ... }
func (s *InMemoryStore) Get(id string) (*Cart, error) { ... }
func (s *InMemoryStore) Save(cart *Cart) error { ... }
```

InMemoryStore implicitly satisfies checkout.CartStore.

## Consequences

### Positive

- **Decoupling**: Producer doesn't control consumer's abstraction
- **Testability**: Easy to create fakes for testing
- **Minimal interfaces**: Only needed methods in interface
- **Multiple interfaces**: Different consumers define different views
- **Less refactoring**: Interface changes only affect consumers that use those methods

### Negative

- **No compile-time check**: Producer doesn't know if it satisfies all consumer interfaces
- **Documentation**: Producer must document which interfaces it's intended to satisfy
- **Duplication**: Similar interfaces in multiple consumers

## Examples

### Checkout Service (Consumer)

```go
// internal/checkout/checkout.go
package checkout

type (
    CartStore interface {
        Get(id string) (*cart.Cart, error)
        Save(cart *cart.Cart) error
    }
    
    InventoryService interface {
        Reserve(reservationID, variantID string, quantity int) (*Reservation, error)
        Release(reservationID string) error
    }
    
    PaymentProvider interface {
        Charge(req ChargeRequest) (ChargeResult, error)
    }
)

func NewService(
    cartStore CartStore,
    inventorySvc InventoryService,
    paymentProvider PaymentProvider,
) *Service {
    return &Service{
        cartStore:       cartStore,
        inventorySvc:    inventorySvc,
        paymentProvider: paymentProvider,
    }
}
```

**Note**: Each interface is minimal. `CartStore` has only 2 methods because that's all `checkout` needs.

### Test Double (Consumer's test package)

```go
// internal/checkout/checkout_test.go
package checkout_test

type FakeCartStore struct {
    carts map[string]*cart.Cart
}

func (f *FakeCartStore) Get(id string) (*cart.Cart, error) {
    return f.carts[id], nil
}

func (f *FakeCartStore) Save(c *cart.Cart) error {
    f.carts[c.ID] = c
    return nil
}

func TestService_PlaceOrder(t *testing.T) {
    fakeStore := &FakeCartStore{carts: make(map[string]*cart.Cart)}
    svc := checkout.NewService(fakeStore, ...)
    // Test checkout logic
}
```

### Anti-Pattern: Producer-Defined Interface

```go
// WRONG: Producer exports large interface
package payment

type Provider interface {
    Charge(req ChargeRequest) (ChargeResult, error)
    GetPayment(id string) (*Payment, error)
    Refund(id string) error
    // ... 10 more methods
}

// Consumer doesn't need Refund or GetPayment
package checkout

func NewService(p payment.Provider) *Service { ... }  // Forced to accept all methods
```

**Problem**: Consumer only calls `Charge`, but must accept unnecessary methods.

## Exceptions

### When Producer Can Define Interface

Producers can export interfaces when:
1. **Multiple implementations exist**: `payment.Provider` has Paystack, M-Pesa, Fake implementations
2. **Interface is stable**: Rarely changes
3. **Consumers need all methods**: Interface is cohesive

**Current example**: `payment.Provider` interface is exported because it's the primary abstraction for multiple implementations.

```go
// internal/payment/payment.go (PRODUCER exports interface)
type Provider interface {
    Charge(req ChargeRequest) (ChargeResult, error)
    GetPayment(id string) (*Payment, error)
}
```

**Why acceptable**: All consumers (checkout, routes, tests) need the same methods. Interface is stable.

### When Consumer Defines Interface

Consumers should define interfaces when:
1. **Single consumer**: Only one package uses it
2. **Subset of methods**: Consumer doesn't need all producer methods
3. **Testing**: Consumer needs test-specific interface

**Current example**: `checkout.CartStore` (should be consumer-defined, not producer-defined)

```go
// SHOULD BE: internal/checkout/checkout.go
type CartStore interface {
    Get(id string) (*cart.Cart, error)
    Save(cart *cart.Cart) error
}

// NOT THIS: internal/cart/cart.go
type Store interface { ... }  // Producer shouldn't export this
```

## Migration Path

To adopt this pattern fully:

1. **Identify**: Find producer-exported interfaces (e.g., `cart.Store`)
2. **Review**: Check if multiple consumers need the same interface
3. **Move**: If single consumer, move interface to consumer package
4. **Rename**: Use specific name (e.g., `checkout.CartStore` not `Store`)

**Phase 2 task**: Move `cart.Store` to `checkout.CartStore` and `routes.CartStore`.

## References

- [Go Proverbs: Accept interfaces, return structs](https://go-proverbs.github.io/)
- [Go Blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
- [Effective Go: Interfaces](https://golang.org/doc/effective_go#interfaces)
- [Dave Cheney: SOLID Go Design](https://dave.cheney.net/2016/08/20/solid-go-design)

## Summary

| Pattern | Producer | Consumer | Use Case |
|---------|----------|----------|----------|
| Producer-defined | Defines interface | Imports interface | Multiple implementations, stable interface |
| Consumer-defined | Implements implicitly | Defines interface | Single consumer, subset of methods |

**Default rule**: Consumer defines interface. Exception: Stable, widely-used abstractions like `payment.Provider`.
