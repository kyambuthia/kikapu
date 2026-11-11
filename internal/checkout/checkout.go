package checkout

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/events"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/obs"
)

var (
	ErrEmptyCart             = errors.New("cart is empty")
	ErrIdempotencyKeyMissing = errors.New("idempotency key is required")
	ErrOrderNotFound         = errors.New("order not found")
)

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusCancelled OrderStatus = "cancelled"
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

type Service struct {
	cartStore       cart.Store
	inventoryStore  inventory.Store
	paymentProvider payment.Provider
	idempotency     *IdempotencyStore
	outbox          events.Outbox
	reservationTTL  time.Duration
}

func NewService(
	cartStore cart.Store,
	inventoryStore inventory.Store,
	paymentProvider payment.Provider,
	idempotencyStore *IdempotencyStore,
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
	ctx := context.Background()

	if idempotencyKey == "" {
		obs.CheckoutTotal.WithLabelValues("error").Inc()
		return nil, ErrIdempotencyKeyMissing
	}

	if existingOrder, ok := s.idempotency.GetOrderForKey(idempotencyKey); ok {
		obs.CheckoutTotal.WithLabelValues("success").Inc()
		return existingOrder, nil
	}

	c, err := s.cartStore.Get(cartID)
	if err != nil {
		obs.CheckoutTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	if len(c.Lines) == 0 {
		obs.CheckoutTotal.WithLabelValues("error").Inc()
		return nil, ErrEmptyCart
	}

	total, err := c.Total()
	if err != nil {
		obs.CheckoutTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	reservations := make([]string, 0, len(c.Lines))
	for _, line := range c.Lines {
		res, err := s.inventoryStore.Reserve(ctx, line.VariantID, line.Quantity, s.reservationTTL)
		if err != nil {
			s.releaseAll(ctx, reservations)
			obs.CheckoutTotal.WithLabelValues("stock_conflict").Inc()
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
		obs.CheckoutTotal.WithLabelValues("declined").Inc()
		return nil, err
	}

	for _, resID := range reservations {
		if err := s.inventoryStore.Commit(ctx, resID); err != nil {
		}
	}

	order := &Order{
		ID:           fmt.Sprintf("ord_%d", time.Now().UnixNano()),
		CartID:       cartID,
		Status:       OrderStatusPaid,
		Total:        total.Value,
		Currency:     total.Currency,
		PaymentID:    chargeResult.ID,
		Reservations: reservations,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.idempotency.SaveOrder(idempotencyKey, order); err != nil {
		obs.CheckoutTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	if s.outbox != nil {
		createdEvent, err := events.NewEvent(events.EventTypeOrderCreated, order.ID, map[string]interface{}{
			"order_id":   order.ID,
			"cart_id":    cartID,
			"status":     string(order.Status),
			"total":      order.Total,
			"currency":   order.Currency,
			"created_at": order.CreatedAt,
		})
		if err == nil {
			s.outbox.Append(ctx, nil, createdEvent)
		}

		paidEvent, err := events.NewEvent(events.EventTypeOrderPaid, order.ID, map[string]interface{}{
			"order_id":    order.ID,
			"payment_id":  chargeResult.ID,
			"total":       order.Total,
			"currency":    order.Currency,
			"captured_at": time.Now(),
		})
		if err == nil {
			s.outbox.Append(ctx, nil, paidEvent)
		}
	}

	c.Clear()
	if err := s.cartStore.Save(c); err != nil {
	}

	obs.CheckoutTotal.WithLabelValues("success").Inc()
	return order, nil
}

func (s *Service) releaseAll(ctx context.Context, reservations []string) {
	for _, resID := range reservations {
		s.inventoryStore.Release(ctx, resID)
	}
}

func (s *Service) GetOrder(orderID string) (*Order, error) {
	return s.idempotency.GetOrder(orderID)
}
