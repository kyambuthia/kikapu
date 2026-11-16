package checkout

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/payment"
)

var (
	ErrEmptyCart             = errors.New("cart is empty")
	ErrIdempotencyKeyMissing = errors.New("idempotency key is required")
	ErrOrderNotFound         = errors.New("order not found")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with a different cart")
)

type OrderStatus string

const (
	OrderStatusPending                         OrderStatus = "pending"
	OrderStatusPaid                            OrderStatus = "paid"
	OrderStatusFailed                          OrderStatus = "failed"
	OrderStatusCancelled                       OrderStatus = "cancelled"
	OrderStatusPaymentCapturedFulfilmentFailed OrderStatus = "payment_captured_fulfilment_failed"
)

type Order struct {
	ID           string
	CartID       string
	Status       OrderStatus
	Total        int64
	Currency     string
	PaymentID    string
	Reservations []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type IdempotencyStore struct {
	mu     sync.RWMutex
	keys   map[string]string
	orders map[string]*Order
}

func NewIdempotencyStore() *IdempotencyStore {
	return &IdempotencyStore{
		keys:   make(map[string]string),
		orders: make(map[string]*Order),
	}
}

func (s *IdempotencyStore) GetOrderForKey(key string) (*Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orderID, ok := s.keys[key]
	if !ok {
		return nil, false
	}

	order, ok := s.orders[orderID]
	if !ok {
		return nil, false
	}

	orderCopy := *order
	orderCopy.Reservations = make([]string, len(order.Reservations))
	copy(orderCopy.Reservations, order.Reservations)
	return &orderCopy, true
}

func (s *IdempotencyStore) SaveOrder(key string, order *Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingOrderID, ok := s.keys[key]; ok {
		existingOrder, orderExists := s.orders[existingOrderID]
		if orderExists {
			orderCopy := *existingOrder
			orderCopy.Reservations = make([]string, len(existingOrder.Reservations))
			copy(orderCopy.Reservations, existingOrder.Reservations)
			*order = orderCopy
			return nil
		}
	}

	s.keys[key] = order.ID
	orderCopy := *order
	orderCopy.Reservations = make([]string, len(order.Reservations))
	copy(orderCopy.Reservations, order.Reservations)
	s.orders[order.ID] = &orderCopy
	return nil
}

func (s *IdempotencyStore) GetOrder(id string) (*Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[id]
	if !ok {
		return nil, ErrOrderNotFound
	}

	orderCopy := *order
	orderCopy.Reservations = make([]string, len(order.Reservations))
	copy(orderCopy.Reservations, order.Reservations)
	return &orderCopy, nil
}

// OrderStore persists checkout orders and idempotency key mappings.
type OrderStore interface {
	GetOrderForKey(key string) (*Order, bool)
	SaveOrder(key string, order *Order) error
	GetOrder(id string) (*Order, error)
}

// TransactionalOrderStore is optionally implemented by stores that can persist
// an order and its outbox events in a single atomic database transaction.
type TransactionalOrderStore interface {
	SaveOrderWithEvents(ctx context.Context, key string, order *Order, ob events.Outbox, evs ...events.Event) error
}

type Service struct {
	cartStore       cart.Store
	inventoryStore  inventory.Store
	paymentProvider payment.Provider
	idempotency     OrderStore
	outbox          events.Outbox
	reservationTTL  time.Duration
}

func NewService(
	cartStore cart.Store,
	inventoryStore inventory.Store,
	paymentProvider payment.Provider,
	idempotencyStore OrderStore,
	outbox events.Outbox,
) *Service {
	return &Service{
		cartStore:       cartStore,
		inventoryStore:  inventoryStore,
		paymentProvider: paymentProvider,
		idempotency:     idempotencyStore,
		outbox:          outbox,
		reservationTTL:  15 * time.Minute,
	}
}

func (s *Service) PlaceOrder(cartID, idempotencyKey string) (*Order, error) {
	return s.PlaceOrderContext(context.Background(), cartID, idempotencyKey)
}

func (s *Service) PlaceOrderContext(ctx context.Context, cartID, idempotencyKey string) (*Order, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if existingOrder, ok := s.idempotency.GetOrderForKey(idempotencyKey); ok {
		if existingOrder.CartID != cartID {
			return nil, ErrIdempotencyConflict
		}
		return existingOrder, nil
	}

	c, err := s.cartStore.Get(cartID)
	if err != nil {
		return nil, err
	}

	if len(c.Lines) == 0 {
		return nil, ErrEmptyCart
	}

	total, err := c.Total()
	if err != nil {
		return nil, err
	}

	reservations := make([]string, 0, len(c.Lines))
	for _, line := range c.Lines {
		res, err := s.inventoryStore.Reserve(ctx, line.VariantID, line.Quantity, s.reservationTTL)
		if err != nil {
			s.releaseAll(ctx, reservations)
			return nil, err
		}
		reservations = append(reservations, res.ID)
	}

	chargeResult, err := s.paymentProvider.Charge(payment.ChargeRequest{
		IDempotencyKey: idempotencyKey,
		Amount:         total,
		CustomerID:     cartID,
	})
	if err != nil {
		s.releaseAll(ctx, reservations)
		return nil, err
	}

	commitErrors := make([]error, 0, len(reservations))
	for _, resID := range reservations {
		if err := s.inventoryStore.Commit(ctx, resID); err != nil {
			commitErrors = append(commitErrors, err)
		}
	}

	orderStatus := OrderStatusPaid
	if len(commitErrors) > 0 {
		orderStatus = OrderStatusPaymentCapturedFulfilmentFailed
	}

	now := time.Now().UTC()
	order := &Order{
		ID:           "ord_" + uuid.NewString(),
		CartID:       cartID,
		Status:       orderStatus,
		Total:        total.Value,
		Currency:     total.Currency,
		PaymentID:    chargeResult.ID,
		Reservations: reservations,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	pendingEvents := s.buildOrderEvents(order, cartID, chargeResult.ID, len(commitErrors))

	if txStore, ok := s.idempotency.(TransactionalOrderStore); ok && s.outbox != nil {
		if err := txStore.SaveOrderWithEvents(ctx, idempotencyKey, order, s.outbox, pendingEvents...); err != nil {
			return nil, err
		}
	} else {
		if err := s.idempotency.SaveOrder(idempotencyKey, order); err != nil {
			return nil, err
		}
		if s.outbox != nil {
			for i := range pendingEvents {
				_ = s.outbox.Append(ctx, nil, pendingEvents[i])
			}
		}
	}

	c.Clear()
	// Best-effort cart cleanup; the order is already persisted and paid.
	_ = s.cartStore.Save(c)

	return order, nil
}

func (s *Service) buildOrderEvents(order *Order, cartID, paymentID string, commitErrCount int) []events.Event {
	if s.outbox == nil {
		return nil
	}

	var out []events.Event
	if createdEvent, err := events.NewEvent(events.EventTypeOrderCreated, order.ID, map[string]interface{}{
		"order_id":   order.ID,
		"cart_id":    cartID,
		"status":     string(order.Status),
		"total":      order.Total,
		"currency":   order.Currency,
		"created_at": order.CreatedAt,
	}); err == nil {
		out = append(out, createdEvent)
	}

	if order.Status == OrderStatusPaymentCapturedFulfilmentFailed {
		if failedEvent, err := events.NewEvent(events.EventTypeOrderFulfilmentFailed, order.ID, map[string]interface{}{
			"order_id":      order.ID,
			"payment_id":    paymentID,
			"total":         order.Total,
			"currency":      order.Currency,
			"commit_errors": commitErrCount,
			"failed_at":     time.Now().UTC(),
		}); err == nil {
			out = append(out, failedEvent)
		}
	} else {
		if paidEvent, err := events.NewEvent(events.EventTypeOrderPaid, order.ID, map[string]interface{}{
			"order_id":    order.ID,
			"payment_id":  paymentID,
			"total":       order.Total,
			"currency":    order.Currency,
			"captured_at": time.Now().UTC(),
		}); err == nil {
			out = append(out, paidEvent)
		}
	}

	return out
}

func (s *Service) releaseAll(ctx context.Context, reservations []string) {
	for _, resID := range reservations {
		_ = s.inventoryStore.Release(ctx, resID)
	}
}

func (s *Service) GetOrder(orderID string) (*Order, error) {
	return s.idempotency.GetOrder(orderID)
}

// ReconcilePayment updates an order's status from an asynchronous payment webhook/callback.
func (s *IdempotencyStore) ReconcilePayment(_ context.Context, paymentID string, state payment.State) error {
	if paymentID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	targetStatus := mapPaymentStateToOrderStatus(state)
	if targetStatus == "" {
		return nil
	}
	for _, o := range s.orders {
		if o.PaymentID == paymentID && o.Status != OrderStatusPaymentCapturedFulfilmentFailed {
			o.Status = targetStatus
			o.UpdatedAt = time.Now().UTC()
		}
	}
	return nil
}

func mapPaymentStateToOrderStatus(state payment.State) OrderStatus {
	switch state {
	case payment.StateCaptured:
		return OrderStatusPaid
	case payment.StateFailed:
		return OrderStatusFailed
	case payment.StateRefunded:
		return OrderStatusCancelled
	default:
		return ""
	}
}
