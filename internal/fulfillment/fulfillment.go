package fulfillment

import (
	"errors"
	"net/url"
	"time"
)

var (
	ErrInvalidTransition     = errors.New("invalid fulfillment status transition")
	ErrInvalidStatus         = errors.New("invalid fulfillment status")
	ErrOrderNotConfirmed     = errors.New("order must be confirmed before fulfillment")
	ErrOverFulfillment       = errors.New("fulfillment items exceed ordered quantity")
	ErrDuplicateFulfillment  = errors.New("variant already fulfilled")
	ErrInvalidTrackingURL    = errors.New("tracking URL must be https")
	ErrInvalidTrackingNumber = errors.New("tracking number cannot be empty")
	ErrInvalidCarrier        = errors.New("carrier cannot be empty")
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
	VariantID string
	Quantity  int
}

type Tracking struct {
	Carrier string
	Number  string
	URL     string
}

type Fulfillment struct {
	ID        string
	OrderID   string
	Status    Status
	Items     []Item
	Tracking  *Tracking
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (f *Fulfillment) Transition(to Status) error {
	if !isValidTransition(f.Status, to) {
		return ErrInvalidTransition
	}

	f.Status = to
	f.UpdatedAt = time.Now()
	return nil
}

func (f *Fulfillment) AddTracking(t Tracking) error {
	if !isHTTPS(t.URL) {
		return ErrInvalidTrackingURL
	}
	if t.Number == "" {
		return ErrInvalidTrackingNumber
	}
	if t.Carrier == "" {
		return ErrInvalidCarrier
	}

	f.Tracking = &t
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

func isHTTPS(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "https"
}

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusPicked, StatusPacked, StatusShipped, StatusDelivered, StatusCancelled:
		return true
	default:
		return false
	}
}

type OrderInfo struct {
	ID     string
	Status string
	Lines  []OrderLine
}

type OrderLine struct {
	VariantID string
	Quantity  int
}
