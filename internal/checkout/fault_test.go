package checkout

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type ScriptableProvider struct {
	mu              sync.Mutex
	calls           int
	payments        map[string]*payment.Payment
	idempotencyKeys map[string]string
	shouldFail      func(callNum int) error
}

func NewScriptableProvider() *ScriptableProvider {
	return &ScriptableProvider{
		payments:        make(map[string]*payment.Payment),
		idempotencyKeys: make(map[string]string),
	}
}

func (p *ScriptableProvider) SetFailure(fn func(callNum int) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shouldFail = fn
}

func (p *ScriptableProvider) Charge(req payment.ChargeRequest) (payment.ChargeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls++

	if paymentID, ok := p.idempotencyKeys[req.IDempotencyKey]; ok {
		pm := p.payments[paymentID]
		return payment.ChargeResult{
			ID:      paymentID,
			Status:  pm.State,
			Message: "duplicate",
		}, nil
	}

	if p.shouldFail != nil {
		if err := p.shouldFail(p.calls); err != nil {
			return payment.ChargeResult{}, err
		}
	}

	paymentID := generatePaymentID()
	pm := &payment.Payment{
		ID:        paymentID,
		State:     payment.StateAuthorized,
		Amount:    req.Amount,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	p.payments[paymentID] = pm
	p.idempotencyKeys[req.IDempotencyKey] = paymentID

	return payment.ChargeResult{
		ID:      paymentID,
		Status:  payment.StateAuthorized,
		Message: "authorized",
	}, nil
}

func (p *ScriptableProvider) GetPayment(id string) (*payment.Payment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pm, ok := p.payments[id]
	if !ok {
		return nil, errors.New("payment not found")
	}
	return pm, nil
}

func (p *ScriptableProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *ScriptableProvider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = 0
}

type ScriptableInventory struct {
	mu             sync.Mutex
	levels         map[string]map[string]*inventory.Level
	reservations   map[string]*inventory.Reservation
	shouldFail     func(op string, variantID string) error
	reserveCallNum int
	commitCallNum  int
	releaseCallNum int
	releaseCalls   []string
	clock          inventory.Clock
}

func NewScriptableInventory(clock inventory.Clock) *ScriptableInventory {
	return &ScriptableInventory{
		levels:       make(map[string]map[string]*inventory.Level),
		reservations: make(map[string]*inventory.Reservation),
		clock:        clock,
	}
}

func (s *ScriptableInventory) SetLevel(variantID, locationID string, onHand int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.levels[variantID] == nil {
		s.levels[variantID] = make(map[string]*inventory.Level)
	}
	s.levels[variantID][locationID] = &inventory.Level{
		VariantID:  variantID,
		LocationID: locationID,
		OnHand:     onHand,
		Reserved:   0,
	}
}

func (s *ScriptableInventory) SetFailure(fn func(op string, variantID string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shouldFail = fn
}

func (s *ScriptableInventory) Levels(ctx context.Context, variantID string) ([]inventory.Level, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	locMap, ok := s.levels[variantID]
	if !ok {
		return nil, nil
	}

	result := make([]inventory.Level, 0, len(locMap))
	for _, l := range locMap {
		result = append(result, inventory.Level{
			VariantID:  l.VariantID,
			LocationID: l.LocationID,
			OnHand:     l.OnHand,
			Reserved:   l.Reserved,
		})
	}
	return result, nil
}

func (s *ScriptableInventory) Reserve(ctx context.Context, variantID string, qty int, ttl time.Duration) (inventory.Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reserveCallNum++

	if s.shouldFail != nil {
		if err := s.shouldFail("reserve", variantID); err != nil {
			return inventory.Reservation{}, err
		}
	}

	locMap, ok := s.levels[variantID]
	if !ok {
		return inventory.Reservation{}, inventory.ErrInsufficientStock
	}

	var bestLocation *inventory.Level
	for _, l := range locMap {
		available := l.OnHand - l.Reserved
		if available >= qty {
			if bestLocation == nil || available < (bestLocation.OnHand-bestLocation.Reserved) {
				bestLocation = l
			}
		}
	}

	if bestLocation == nil {
		return inventory.Reservation{}, inventory.ErrInsufficientStock
	}

	now := s.clock.Now()
	reservation := &inventory.Reservation{
		ID:         generateReservationID(),
		VariantID:  variantID,
		LocationID: bestLocation.LocationID,
		Qty:        qty,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}

	bestLocation.Reserved += qty
	s.reservations[reservation.ID] = reservation

	return inventory.Reservation{
		ID:         reservation.ID,
		VariantID:  reservation.VariantID,
		LocationID: reservation.LocationID,
		Qty:        reservation.Qty,
		ExpiresAt:  reservation.ExpiresAt,
		CreatedAt:  reservation.CreatedAt,
	}, nil
}

func (s *ScriptableInventory) Release(ctx context.Context, reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.releaseCallNum++
	s.releaseCalls = append(s.releaseCalls, reservationID)

	res, ok := s.reservations[reservationID]
	if !ok {
		return inventory.ErrReservationNotFound
	}

	locMap, ok := s.levels[res.VariantID]
	if !ok {
		delete(s.reservations, reservationID)
		return nil
	}

	loc, ok := locMap[res.LocationID]
	if ok && loc.Reserved >= res.Qty {
		loc.Reserved -= res.Qty
	}

	delete(s.reservations, reservationID)
	return nil
}

func (s *ScriptableInventory) Commit(ctx context.Context, reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commitCallNum++

	if s.shouldFail != nil {
		if err := s.shouldFail("commit", ""); err != nil {
			return err
		}
	}

	res, ok := s.reservations[reservationID]
	if !ok {
		return inventory.ErrReservationNotFound
	}

	now := s.clock.Now()
	if res.ExpiresAt.Before(now) {
		locMap, ok := s.levels[res.VariantID]
		if ok {
			loc, ok := locMap[res.LocationID]
			if ok && loc.Reserved >= res.Qty {
				loc.Reserved -= res.Qty
			}
		}
		delete(s.reservations, reservationID)
		return inventory.ErrReservationExpired
	}

	locMap, ok := s.levels[res.VariantID]
	if !ok {
		delete(s.reservations, reservationID)
		return inventory.ErrReservationNotFound
	}

	loc, ok := locMap[res.LocationID]
	if !ok {
		delete(s.reservations, reservationID)
		return inventory.ErrReservationNotFound
	}

	loc.OnHand -= res.Qty
	loc.Reserved -= res.Qty

	delete(s.reservations, reservationID)
	return nil
}

func (s *ScriptableInventory) ExpireDue(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, res := range s.reservations {
		if res.ExpiresAt.Before(now) {
			locMap, ok := s.levels[res.VariantID]
			if ok {
				loc, ok := locMap[res.LocationID]
				if ok && loc.Reserved >= res.Qty {
					loc.Reserved -= res.Qty
				}
			}
			delete(s.reservations, id)
			count++
		}
	}
	return count, nil
}

func (s *ScriptableInventory) ReserveCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserveCallNum
}

func (s *ScriptableInventory) CommitCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitCallNum
}

func (s *ScriptableInventory) ReleaseCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releaseCallNum
}

func (s *ScriptableInventory) ReleaseCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.releaseCalls))
	copy(result, s.releaseCalls)
	return result
}

func (s *ScriptableInventory) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserveCallNum = 0
	s.commitCallNum = 0
	s.releaseCallNum = 0
	s.releaseCalls = nil
}

type SpyOutbox struct {
	mu          sync.Mutex
	appended    []events.Event
	sent        []string
	failed      []string
	markSentErr error
	shouldFail  bool
}

func NewSpyOutbox() *SpyOutbox {
	return &SpyOutbox{
		appended: make([]events.Event, 0),
		sent:     make([]string, 0),
		failed:   make([]string, 0),
	}
}

func (o *SpyOutbox) Append(ctx context.Context, tx events.Tx, e events.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.shouldFail {
		return errors.New("outbox failure")
	}

	o.appended = append(o.appended, e)
	return nil
}

func (o *SpyOutbox) Claim(ctx context.Context, limit int) ([]events.Record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var records []events.Record
	for _, e := range o.appended {
		records = append(records, events.Record{
			ID:     e.ID,
			Event:  e,
			Status: events.StatusPending,
		})
	}
	return records, nil
}

func (o *SpyOutbox) MarkSent(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.markSentErr != nil {
		return o.markSentErr
	}

	o.sent = append(o.sent, id)
	return nil
}

func (o *SpyOutbox) MarkFailed(ctx context.Context, id string, err error, nextAttempt time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.failed = append(o.failed, id)
	return nil
}

func (o *SpyOutbox) AppendedCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.appended)
}

func (o *SpyOutbox) Appended() []events.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]events.Event, len(o.appended))
	copy(result, o.appended)
	return result
}

func (o *SpyOutbox) Sent() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]string, len(o.sent))
	copy(result, o.sent)
	return result
}

func (o *SpyOutbox) SetMarkSentError(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.markSentErr = err
}

func generatePaymentID() string {
	return "pay_" + time.Now().Format("20060102150405.999999999")
}

func generateReservationID() string {
	return "res_" + time.Now().Format("20060102150405.999999999")
}

func TestPaymentProviderTimeout_ReleasesReservations(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := NewScriptableProvider()
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	paymentProvider.SetFailure(func(callNum int) error {
		if callNum == 1 {
			return payment.ErrTimeout
		}
		return nil
	})

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-timeout")
	if err == nil {
		t.Fatal("Expected error on payment timeout")
	}

	if !errors.Is(err, payment.ErrTimeout) {
		t.Errorf("Error = %v, want %v", err, payment.ErrTimeout)
	}

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if len(levels) == 0 {
		t.Fatal("Expected inventory levels")
	}

	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0 (reservation should be released)", levels[0].Reserved)
	}

	if levels[0].OnHand != 10 {
		t.Errorf("OnHand = %d, want 10 (stock should remain unchanged)", levels[0].OnHand)
	}

	if inventoryStore.ReleaseCallCount() != 1 {
		t.Errorf("ReleaseCallCount = %d, want 1", inventoryStore.ReleaseCallCount())
	}

	res, err := inventoryStore.Reserve(ctx, "var001", 10, time.Minute)
	if err != nil {
		t.Error("Stock should be available after reservation release")
	}
	if res.ID == "" {
		t.Error("Expected valid reservation ID")
	}
}

func TestPaymentDeclined_ReleasesReservationsAndCartIntact(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := NewScriptableProvider()
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	paymentProvider.SetFailure(func(callNum int) error {
		return payment.ErrChargeFailed
	})

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-declined")
	if err == nil {
		t.Fatal("Expected error on payment declined")
	}

	if !errors.Is(err, payment.ErrChargeFailed) {
		t.Errorf("Error = %v, want %v", err, payment.ErrChargeFailed)
	}

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0", levels[0].Reserved)
	}

	storedCart, err := cartStore.Get("cart001")
	if err != nil {
		t.Fatalf("Get cart error: %v", err)
	}

	if len(storedCart.Lines) != 1 {
		t.Errorf("Cart should still have 1 item, got %d items", len(storedCart.Lines))
	}

	if storedCart.Lines[0].VariantID != "var001" {
		t.Errorf("Cart item variant = %s, want var001", storedCart.Lines[0].VariantID)
	}

	if storedCart.Lines[0].Quantity != 2 {
		t.Errorf("Cart item quantity = %d, want 2", storedCart.Lines[0].Quantity)
	}
}

func TestInventoryCommitFailsAfterCapture_OperationFails(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	spyOutbox := NewSpyOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	inventoryStore.SetFailure(func(op string, variantID string) error {
		if op == "commit" {
			return errors.New("inventory commit failed")
		}
		return nil
	})

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, spyOutbox)

	order, err := svc.PlaceOrder("cart001", "idem-key-commit-fail")
	if err != nil {
		t.Fatalf("PlaceOrder error: %v", err)
	}

	if order.Status != OrderStatusPaymentCapturedFulfilmentFailed {
		t.Errorf("Order status = %v, want %v", order.Status, OrderStatusPaymentCapturedFulfilmentFailed)
	}

	if len(order.Reservations) != 1 {
		t.Errorf("Order reservations = %d, want 1", len(order.Reservations))
	}

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if levels[0].Reserved != 2 {
		t.Errorf("Reserved = %d, want 2 (stock reserved but commit failed)", levels[0].Reserved)
	}

	if levels[0].OnHand != 10 {
		t.Errorf("OnHand = %d, want 10 (stock not decremented due to commit failure)", levels[0].OnHand)
	}

	appendedEvents := spyOutbox.Appended()
	if len(appendedEvents) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(appendedEvents))
	}

	orderCreatedFound := false
	fulfilmentFailedFound := false
	for _, e := range appendedEvents {
		if e.Type == events.EventTypeOrderCreated {
			orderCreatedFound = true
		}
		if e.Type == events.EventTypeOrderFulfilmentFailed {
			fulfilmentFailedFound = true
		}
	}

	if !orderCreatedFound {
		t.Error("Expected OrderCreated event to be emitted")
	}

	if !fulfilmentFailedFound {
		t.Error("Expected OrderFulfilmentFailed event to be emitted")
	}
}

func TestPartialReservationFailure_RollsBackAllReservations(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)
	inventoryStore.SetLevel("var002", "loc1", 5)
	inventoryStore.SetLevel("var003", "loc1", 3)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	c.AddItem("var002", 1, money.NewAmount(500, "USD"))
	c.AddItem("var003", 5, money.NewAmount(700, "USD"))
	cartStore.Save(c)

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-partial-fail")
	if err == nil {
		t.Fatal("Expected error on insufficient stock for var003")
	}

	if !errors.Is(err, inventory.ErrInsufficientStock) {
		t.Errorf("Error = %v, want %v", err, inventory.ErrInsufficientStock)
	}

	levels1, _ := inventoryStore.Levels(ctx, "var001")
	if levels1[0].Reserved != 0 {
		t.Errorf("var001 Reserved = %d, want 0 (rolled back)", levels1[0].Reserved)
	}

	levels2, _ := inventoryStore.Levels(ctx, "var002")
	if levels2[0].Reserved != 0 {
		t.Errorf("var002 Reserved = %d, want 0 (rolled back)", levels2[0].Reserved)
	}

	if inventoryStore.ReleaseCallCount() != 2 {
		t.Errorf("ReleaseCallCount = %d, want 2 (rolled back 2 successful reserves)", inventoryStore.ReleaseCallCount())
	}

	if paymentProvider.GetCallCount() != 0 {
		t.Errorf("Payment provider should not be called, got %d calls", paymentProvider.GetCallCount())
	}
}

func TestReservationRelease_GracefullyHandlesNotFound(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := NewScriptableProvider()
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 10)

	c := cart.NewCart("cart001", "USD")
	c.AddItem("var001", 2, money.NewAmount(1000, "USD"))
	cartStore.Save(c)

	paymentProvider.SetFailure(func(callNum int) error {
		return payment.ErrTimeout
	})

	svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	_, err := svc.PlaceOrder("cart001", "idem-key-test")
	if err == nil {
		t.Fatal("Expected error")
	}

	levels, _ := inventoryStore.Levels(ctx, "var001")
	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0", levels[0].Reserved)
	}
}

func TestPaymentTimeout_StockLevelsMatch(t *testing.T) {
	ctx := context.Background()
	cartStore := cart.NewInMemoryStore()
	clock := &inventory.FakeClock{Current: time.Now()}
	inventoryStore := NewScriptableInventory(clock)
	paymentProvider := NewScriptableProvider()
	idempotencyStore := NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()

	inventoryStore.SetLevel("var001", "loc1", 100)

	cartIDs := []string{"cart001", "cart002", "cart003"}
	for _, cartID := range cartIDs {
		c := cart.NewCart(cartID, "USD")
		c.AddItem("var001", 5, money.NewAmount(1000, "USD"))
		cartStore.Save(c)

		originalLevels, _ := inventoryStore.Levels(ctx, "var001")

		paymentProvider.SetFailure(func(callNum int) error {
			return payment.ErrTimeout
		})

		svc := NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)
		_, err := svc.PlaceOrder(cartID, "idem-key-"+cartID)
		if err == nil {
			t.Fatalf("Expected timeout error for %s", cartID)
		}

		newLevels, _ := inventoryStore.Levels(ctx, "var001")
		if newLevels[0].OnHand != originalLevels[0].OnHand {
			t.Errorf("OnHand changed from %d to %d for %s", originalLevels[0].OnHand, newLevels[0].OnHand, cartID)
		}

		if newLevels[0].Reserved != 0 {
			t.Errorf("Reserved = %d for %s, want 0", newLevels[0].Reserved, cartID)
		}

		paymentProvider.Reset()
		idempotencyStore = NewIdempotencyStore()
	}
}
