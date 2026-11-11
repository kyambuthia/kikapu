package fulfillment

import (
	"errors"
	"net/url"
	"time"
)

var (
	ErrInvalidTransition     = errors.New("invalid fulfillment status transition")
	ErrOverFulfillment       = errors.New("fulfillment items exceed ordered quantity")
	ErrInvalidStatus         = errors.New("invalid fulfillment status")
	ErrInvalidTrackingURL    = errors.New("tracking URL must be https")
	ErrMissingTrackingNumber = errors.New("tracking number is required")
	ErrMissingCarrier        = errors.New("carrier is required")
	ErrItemsEmpty            = errors.New("fulfillment must have at least one item")
	ErrFulfillmentNotFound   = errors.New("fulfillment not found")
	ErrOrderNotConfirmed     = errors.New("order must be confirmed before fulfillment")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusPicked    Status = "picked"
	StatusPacked    Status = "packed"
	StatusShipped   Status = "shipped"
	StatusDelivered Status = "delivered"
	StatusCancelled Status = "cancelled"
)

type Item struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

type Tracking struct {
	Carrier string `json:"carrier"`
	Number  string `json:"number"`
	URL     string `json:"url"`
}

type Fulfillment struct {
	ID        string    `json:"id"`
	OrderID   string    `json:"order_id"`
	Status    Status    `json:"status"`
	Items     []Item    `json:"items"`
	Tracking  *Tracking `json:"tracking,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (f *Fulfillment) Transition(to Status) error {
	if !isValidTransition(f.Status, to) {
		return ErrInvalidTransition
	}

	f.Status = to
	f.UpdatedAt = time.Now()
	return nil
}

func isValidTransition(from, to Status) bool {
	transitions := map[Status][]Status{
		StatusPending:   {StatusPicked, StatusCancelled},
		StatusPicked:    {StatusPacked, StatusCancelled},
		StatusPacked:    {StatusShipped, StatusCancelled},
		StatusShipped:   {StatusDelivered},
		StatusDelivered: {},
		StatusCancelled: {},
	}

	allowed, exists := transitions[from]
	if !exists {
		return false
	}

	for _, s := range allowed {
		if s == to {
			return true
		}
	}

	return false
}

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusPicked, StatusPacked, StatusShipped, StatusDelivered, StatusCancelled:
		return true
	default:
		return false
	}
}

func ValidateTrackingURL(urlStr string) error {
	if urlStr == "" {
		return nil
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return ErrInvalidTrackingURL
	}

	if parsed.Scheme != "https" {
		return ErrInvalidTrackingURL
	}

	return nil
}

func NewFulfillment(id, orderID string, items []Item) (*Fulfillment, error) {
	if len(items) == 0 {
		return nil, ErrItemsEmpty
	}

	now := time.Now()
	return &Fulfillment{
		ID:        id,
		OrderID:   orderID,
		Status:    StatusPending,
		Items:     items,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

type Service struct {
	repo      Repository
	orderRepo OrderRepository
}

type Repository interface {
	Create(f *Fulfillment) error
	Get(id string) (*Fulfillment, error)
	ListByOrder(orderID string) ([]*Fulfillment, error)
	Update(f *Fulfillment) error
	AddTracking(id string, t Tracking) error
}

type OrderRepository interface {
	Get(id string) (Order, error)
	UpdateFulfillmentStatus(id string, status string) error
}

type Order struct {
	ID            string
	Status        string
	PaymentStatus string
	Lines         []OrderLine
}

type OrderLine struct {
	VariantID string
	Quantity  int
}

func NewService(repo Repository, orderRepo OrderRepository) *Service {
	return &Service{
		repo:      repo,
		orderRepo: orderRepo,
	}
}

func (s *Service) Create(ctx interface{}, orderID string, items []Item) (*Fulfillment, error) {
	order, err := s.orderRepo.Get(orderID)
	if err != nil {
		return nil, err
	}

	if order.Status != "confirmed" {
		return nil, ErrOrderNotConfirmed
	}

	existing, err := s.repo.ListByOrder(orderID)
	if err != nil {
		return nil, err
	}

	orderedQty := make(map[string]int)
	for _, line := range order.Lines {
		orderedQty[line.VariantID] = line.Quantity
	}

	fulfilledQty := make(map[string]int)
	for _, f := range existing {
		if f.Status != StatusCancelled {
			for _, item := range f.Items {
				fulfilledQty[item.VariantID] += item.Quantity
			}
		}
	}

	for _, item := range items {
		available := orderedQty[item.VariantID] - fulfilledQty[item.VariantID]
		if item.Quantity > available {
			return nil, ErrOverFulfillment
		}
	}

	id := generateFulfillmentID()
	fulfillment, err := NewFulfillment(id, orderID, items)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Create(fulfillment); err != nil {
		return nil, err
	}

	return fulfillment, nil
}

func (s *Service) UpdateStatus(ctx interface{}, id string, to Status) error {
	fulfillment, err := s.repo.Get(id)
	if err != nil {
		return err
	}

	if err := fulfillment.Transition(to); err != nil {
		return err
	}

	if err := s.repo.Update(fulfillment); err != nil {
		return nil
	}

	if to == StatusShipped {
		allFulfillments, err := s.repo.ListByOrder(fulfillment.OrderID)
		if err != nil {
			return err
		}

		if allOrderItemsShipped(fulfillment.OrderID, allFulfillments) {
			if err := s.orderRepo.UpdateFulfillmentStatus(fulfillment.OrderID, "fulfilled"); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) AddTracking(ctx interface{}, id string, t Tracking) error {
	if t.Carrier == "" {
		return ErrMissingCarrier
	}
	if t.Number == "" {
		return ErrMissingTrackingNumber
	}
	if err := ValidateTrackingURL(t.URL); err != nil {
		return err
	}

	fulfillment, err := s.repo.Get(id)
	if err != nil {
		return err
	}

	if fulfillment.Status != StatusShipped {
		return ErrInvalidTransition
	}

	return s.repo.AddTracking(id, t)
}

func allOrderItemsShipped(orderID string, fulfillments []*Fulfillment) bool {
	return len(fulfillments) > 0
}

func generateFulfillmentID() string {
	return "ful_" + time.Now().Format("20060102150405") + randomSuffix()
}

func randomSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[time.Now().Nanosecond()%len(charset)]
	}
	return string(b)
}
