package fulfillment

import (
	"context"
	"time"

	"github.com/kyambuthia/kikapu/internal/events"
)

type Repository interface {
	Create(ctx context.Context, f *Fulfillment) error
	Get(ctx context.Context, id string) (*Fulfillment, error)
	Update(ctx context.Context, f *Fulfillment) error
	ListByOrder(ctx context.Context, orderID string) ([]Fulfillment, error)
}

type OrderFetcher interface {
	Get(ctx context.Context, id string) (*OrderInfo, error)
}

type OrderUpdater interface {
	UpdateFulfillmentStatus(ctx context.Context, orderID string, status string, updatedAt time.Time) error
}

type EventOutbox interface {
	Append(ctx context.Context, tx interface{}, e events.Event) error
}

type Service struct {
	repo        Repository
	orderGetter OrderFetcher
	orderStore  OrderUpdater
	outbox      EventOutbox
}

func NewService(repo Repository, orderGetter OrderFetcher) *Service {
	return &Service{
		repo:        repo,
		orderGetter: orderGetter,
	}
}

func (s *Service) SetOrderStore(store OrderUpdater) {
	s.orderStore = store
}

func (s *Service) SetOutbox(outbox EventOutbox) {
	s.outbox = outbox
}

func (s *Service) CreateFulfillment(ctx context.Context, orderID string, items []Item) (*Fulfillment, error) {
	order, err := s.orderGetter.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}

	if order.Status != "confirmed" {
		return nil, ErrOrderNotConfirmed
	}

	if err := s.validateItems(order.Lines, items); err != nil {
		return nil, err
	}

	fulfillments, err := s.repo.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	if err := s.checkOverFulfillment(fulfillments, items, order.Lines); err != nil {
		return nil, err
	}

	now := time.Now()
	f := &Fulfillment{
		OrderID:   orderID,
		Status:    StatusPending,
		Items:     items,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return nil, err
	}

	return f, nil
}

func (s *Service) TransitionStatus(ctx context.Context, id string, to Status) error {
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	if !to.IsValid() {
		return ErrInvalidStatus
	}

	if err := f.Transition(to); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, f); err != nil {
		return err
	}

	if to == StatusShipped {
		if err := s.checkAndUpdateOrderFulfillment(ctx, f.OrderID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) AddTracking(ctx context.Context, id string, t Tracking) error {
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	if err := f.AddTracking(t); err != nil {
		return err
	}

	return s.repo.Update(ctx, f)
}

func (s *Service) Get(ctx context.Context, id string) (*Fulfillment, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) ListByOrder(ctx context.Context, orderID string) ([]Fulfillment, error) {
	return s.repo.ListByOrder(ctx, orderID)
}

func (s *Service) validateItems(orderLines []OrderLine, items []Item) error {
	orderedQty := make(map[string]int)
	for _, line := range orderLines {
		orderedQty[line.VariantID] = line.Quantity
	}

	for _, item := range items {
		if item.Quantity <= 0 {
			return ErrOverFulfillment
		}
		if orderedQty[item.VariantID] == 0 {
			return ErrOverFulfillment
		}
	}

	return nil
}

func (s *Service) checkOverFulfillment(existing []Fulfillment, newItems []Item, orderLines []OrderLine) error {
	fulfilledQty := make(map[string]int)
	for _, f := range existing {
		for _, item := range f.Items {
			fulfilledQty[item.VariantID] += item.Quantity
		}
	}

	orderedQty := make(map[string]int)
	for _, line := range orderLines {
		orderedQty[line.VariantID] = line.Quantity
	}

	for _, item := range newItems {
		if fulfilledQty[item.VariantID]+item.Quantity > orderedQty[item.VariantID] {
			return ErrOverFulfillment
		}
	}

	return nil
}

func (s *Service) checkAndUpdateOrderFulfillment(ctx context.Context, orderID string) error {
	order, err := s.orderGetter.Get(ctx, orderID)
	if err != nil {
		return err
	}

	fulfillments, err := s.repo.ListByOrder(ctx, orderID)
	if err != nil {
		return err
	}

	shippedQty := make(map[string]int)
	for _, f := range fulfillments {
		if f.Status == StatusShipped || f.Status == StatusDelivered {
			for _, item := range f.Items {
				shippedQty[item.VariantID] += item.Quantity
			}
		}
	}

	allShipped := true
	for _, line := range order.Lines {
		if shippedQty[line.VariantID] < line.Quantity {
			allShipped = false
			break
		}
	}

	if allShipped && s.orderStore != nil {
		now := time.Now()
		if err := s.orderStore.UpdateFulfillmentStatus(ctx, orderID, "fulfilled", now); err != nil {
			return err
		}

		if s.outbox != nil {
			evt, err := events.NewEvent(events.EventTypeOrderFulfilled, orderID, map[string]interface{}{
				"order_id":    orderID,
				"fulfilled_at": now,
			})
			if err == nil {
				s.outbox.Append(ctx, nil, evt)
			}
		}
	}

	return nil
}
