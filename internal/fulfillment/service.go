package fulfillment

import (
	"context"
	"time"
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

type Service struct {
	repo        Repository
	orderGetter OrderFetcher
}

func NewService(repo Repository, orderGetter OrderFetcher) *Service {
	return &Service{
		repo:        repo,
		orderGetter: orderGetter,
	}
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

	return s.repo.Update(ctx, f)
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
