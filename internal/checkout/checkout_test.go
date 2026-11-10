package checkout

import (
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestService_PlaceOrder_DoubleSubmit(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	inventorySvc := inventory.NewService(inventory.RealClock{}, 5*time.Minute)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()

	inventorySvc.SetStock("var001", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, payment.money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventorySvc, paymentProvider, idempotencyStore)

	order1, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != nil {
		t.Fatalf("PlaceOrder() error: %v", err)
	}

	order2, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != nil {
		t.Fatalf("PlaceOrder() error: %v", err)
	}

	if order1.ID != order2.ID {
		t.Errorf("Double submit should return same order, got %s and %s", order1.ID, order2.ID)
	}

	if paymentProvider.GetCallCount() != 1 {
		t.Errorf("Payment provider should be called once, got %d", paymentProvider.GetCallCount())
	}
}

func TestService_PlaceOrder_Concurrent(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	inventorySvc := inventory.NewService(inventory.RealClock{}, 5*time.Minute)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()

	inventorySvc.SetStock("var001", 100)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, payment.money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventorySvc, paymentProvider, idempotencyStore)

	var wg sync.WaitGroup
	results := make(chan string, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order, err := svc.PlaceOrder("cart001", "idem-key-001")
			if err != nil {
				t.Errorf("PlaceOrder() error: %v", err)
				return
			}
			results <- order.ID
		}()
	}

	wg.Wait()
	close(results)

	seenOrders := make(map[string]bool)
	for orderID := range results {
		seenOrders[orderID] = true
	}

	if len(seenOrders) != 1 {
		t.Errorf("Concurrent calls should return same order ID, got %d different IDs", len(seenOrders))
	}
}

func TestService_PlaceOrder_DeclinedPayment(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	inventorySvc := inventory.NewService(inventory.RealClock{}, 5*time.Minute)
	paymentProvider := payment.NewFakeProvider(payment.Declined)
	idempotencyStore := NewIdempotencyStore()

	inventorySvc.SetStock("var001", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, payment.money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventorySvc, paymentProvider, idempotencyStore)

	_, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err == nil {
		t.Error("PlaceOrder() should fail with declined payment")
	}

	cartAfter, err := cartStore.Get("cart001")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if len(cartAfter.Lines) != 1 {
		t.Errorf("Cart should remain intact after declined payment, got %d lines", len(cartAfter.Lines))
	}

	_, err = inventorySvc.GetReservation("res_var001")
	if err == nil {
		t.Error("Reservation should be released after declined payment")
	}

	inventorySvc.SetStock("var001", 0)
	_, err = inventorySvc.Reserve("res_test", "var001", 1)
	if err != nil {
		t.Error("Stock should be available again after reservation release")
	}
}

func TestService_PlaceOrder_ExpiredReservation(t *testing.T) {
	clock := &inventory.FakeClock{Current: time.Now()}
	inventorySvc := inventory.NewService(clock, 5*time.Minute)
	
	inventorySvc.SetStock("var001", 5)

	res1, err := inventorySvc.Reserve("res001", "var001", 5)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	clock.Add(6 * time.Minute)

	_, err = inventorySvc.GetReservation(res1.ID)
	if err != inventory.ErrReservationExpired {
		t.Errorf("GetReservation() error = %v, want %v", err, inventory.ErrReservationExpired)
	}

	_, err = inventorySvc.Reserve("res002", "var001", 5)
	if err != nil {
		t.Error("Should be able to reserve after expiration")
	}
}

func TestService_PlaceOrder_EmptyCart(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	inventorySvc := inventory.NewService(inventory.RealClock{}, 5*time.Minute)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()

	c := cart.NewCart("cart001", "USD")
	cartStore.Save(c)

	svc := NewService(cartStore, inventorySvc, paymentProvider, idempotencyStore)

	_, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != ErrEmptyCart {
		t.Errorf("PlaceOrder() error = %v, want %v", err, ErrEmptyCart)
	}
}
