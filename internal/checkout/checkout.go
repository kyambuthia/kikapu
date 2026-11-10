package checkout

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/payment"
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
	ID          string
	CartID      string
	Status      OrderStatus
	Total       int64
	Currency    string
	PaymentID   string
	Reservation string
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
	return &orderCopy, true
}

func (s *IdempotencyStore) SaveOrder(key string, order *Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingOrderID, ok := s.keys[key]; ok {
		existingOrder, orderExists := s.orders[existingOrderID]
		if orderExists {
			orderCopy := *existingOrder
			*order = orderCopy
			return nil
		}
	}

	s.keys[key] = order.ID
	orderCopy := *order
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
	return &orderCopy, nil
}

type Service struct {
	cartStore       cart.Store
	inventorySvc    *inventory.Service
	paymentProvider payment.Provider
	idempotency     *IdempotencyStore
}

func NewService(
	cartStore cart.Store,
	inventorySvc *inventory.Service,
	paymentProvider payment.Provider,
	idempotencyStore *IdempotencyStore,
) *Service {
	return &Service{
		cartStore:       cartStore,
		inventorySvc:    inventorySvc,
		paymentProvider: paymentProvider,
		idempotency:     idempotencyStore,
	}
}

func (s *Service) PlaceOrder(cartID, idempotencyKey string) (*Order, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyMissing
	}

	if existingOrder, ok := s.idempotency.GetOrderForKey(idempotencyKey); ok {
		return existingOrder, nil
	}

	cart, err := s.cartStore.Get(cartID)
	if err != nil {
		return nil, err
	}

	if len(cart.Lines) == 0 {
		return nil, ErrEmptyCart
	}

	total, err := cart.Total()
	if err != nil {
		return nil, err
	}

	reservations := make(map[string]string)
	for _, line := range cart.Lines {
		reservationID := fmt.Sprintf("res_%s_%d", line.VariantID, time.Now().UnixNano())
		_, err := s.inventorySvc.Reserve(reservationID, line.VariantID, line.Quantity)
		if err != nil {
			for resID := range reservations {
				s.inventorySvc.Release(resID)
			}
			return nil, err
		}
		reservations[line.VariantID] = reservationID
	}

	chargeResult, err := s.paymentProvider.Charge(payment.ChargeRequest{
		IDempotencyKey: idempotencyKey,
		Amount:         total,
		CustomerID:     cartID,
	})

	if err != nil {
		for _, resID := range reservations {
			s.inventorySvc.Release(resID)
		}
		return nil, err
	}

	order := &Order{
		ID:        fmt.Sprintf("ord_%d", time.Now().UnixNano()),
		CartID:    cartID,
		Status:    OrderStatusPaid,
		Total:     total.Value,
		Currency:  total.Currency,
		PaymentID: chargeResult.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.idempotency.SaveOrder(idempotencyKey, order); err != nil {
		return nil, err
	}

	cart.Clear()
	if err := s.cartStore.Save(cart); err != nil {
	}

	return order, nil
}

func (s *Service) GetOrder(orderID string) (*Order, error) {
	return s.idempotency.GetOrder(orderID)
}
