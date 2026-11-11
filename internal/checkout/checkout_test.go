package checkout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestService_PlaceOrder_DoubleSubmit(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

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

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if levels[0].OnHand != 8 {
		t.Errorf("OnHand = %d, want 8 (committed)", levels[0].OnHand)
	}
}

func TestService_PlaceOrder_Concurrent(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 100)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

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
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.Declined)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err == nil {
		t.Error("PlaceOrder() should fail with declined payment")
	}

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if levels[0].Reserved != 0 {
		t.Errorf("Reservation should be released after declined payment, reserved = %d", levels[0].Reserved)
	}

	_, err = inventoryStore.Reserve(ctx, "var001", 10, time.Minute)
	if err != nil {
		t.Error("Stock should be available again after reservation release")
	}
}

func TestService_PlaceOrder_CommitsReservations(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)
	inventoryStore.SetLevel("var002", "loc1", 5)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	c.AddItem("var002", 1, money.NewAmount(500, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	order, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != nil {
		t.Fatalf("PlaceOrder() error: %v", err)
	}

	if len(order.Reservations) != 2 {
		t.Errorf("Order should have 2 reservations, got %d", len(order.Reservations))
	}

	levels1, _ := inventoryStore.Levels(ctx, "var001")
	if levels1[0].OnHand != 8 {
		t.Errorf("var001 OnHand = %d, want 8 (committed)", levels1[0].OnHand)
	}
	if levels1[0].Reserved != 0 {
		t.Errorf("var001 Reserved = %d, want 0 (committed)", levels1[0].Reserved)
	}

	levels2, _ := inventoryStore.Levels(ctx, "var002")
	if levels2[0].OnHand != 4 {
		t.Errorf("var002 OnHand = %d, want 4 (committed)", levels2[0].OnHand)
	}
	if levels2[0].Reserved != 0 {
		t.Errorf("var002 Reserved = %d, want 0 (committed)", levels2[0].Reserved)
	}
}

func TestService_PlaceOrder_InsufficientStock(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)
	inventoryStore.SetLevel("var002", "loc1", 2)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 5, money.NewAmount(1000, "USD"))
	c.AddItem("var002", 5, money.NewAmount(500, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != inventory.ErrInsufficientStock {
		t.Errorf("PlaceOrder() error = %v, want %v", err, inventory.ErrInsufficientStock)
	}

	levels1, _ := inventoryStore.Levels(ctx, "var001")
	if levels1[0].Reserved != 0 {
		t.Errorf("var001 reservation should be released, reserved = %d", levels1[0].Reserved)
	}

	levels2, _ := inventoryStore.Levels(ctx, "var002")
	if levels2[0].Reserved != 0 {
		t.Errorf("var002 reservation should be released, reserved = %d", levels2[0].Reserved)
	}

	if paymentProvider.GetCallCount() != 0 {
		t.Errorf("Payment provider should not be called when stock insufficient, got %d calls", paymentProvider.GetCallCount())
	}
}

func TestService_PlaceOrder_EmptyCart(t *testing.T) {
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	c := cart.NewCart("cart001", "USD")
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-001")
	if err != ErrEmptyCart {
		t.Errorf("PlaceOrder() error = %v, want %v", err, ErrEmptyCart)
	}
}

func TestService_PlaceOrder_EventsAppended(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	order, err := svc.PlaceOrder("cart001", "idem-key-events")
	if err != nil {
		t.Fatalf("PlaceOrder() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	records, err := eventOutbox.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("Claim() error: %v", err)
	}

	if len(records) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(records))
	}

	eventTypes := make(map[string]bool)
	for _, r := range records {
		eventTypes[r.Event.Type] = true
	}

	if !eventTypes["order.created"] {
		t.Error("Expected order.created event")
	}

	if !eventTypes["order.paid"] {
		t.Error("Expected order.paid event")
	}

	for _, r := range records {
		if r.Event.AggregateID != order.ID {
			t.Errorf("Event aggregate ID = %v, want %v", r.Event.AggregateID, order.ID)
		}
	}
}

func TestOutboxAtomic(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := inventory.NewMemoryStore(clock)
	paymentProvider := payment.NewFakeProvider(payment.Declined)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-fail")
	if err == nil {
		t.Error("Expected place order to fail")
	}

	time.Sleep(100 * time.Millisecond)

	records, err := eventOutbox.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("Claim() error: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Claim() returned %d events, want 0 (no events should exist due to failed order)", len(records))
	}
}
