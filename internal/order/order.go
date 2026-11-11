package order

import (
	"errors"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidTransition     = errors.New("invalid status transition")
	ErrTotalsLocked          = errors.New("totals are locked after payment captured")
	ErrRefundExceedsCaptured = errors.New("refund amount exceeds captured amount")
	ErrInvalidStatus         = errors.New("invalid order status")
	ErrInvalidPaymentStatus  = errors.New("invalid payment status")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusFulfilled Status = "fulfilled"
	StatusCancelled Status = "cancelled"
	StatusRefunded  Status = "refunded"
)

type PaymentStatus string

const (
	PaymentStatusPending           PaymentStatus = "pending"
	PaymentStatusCaptured          PaymentStatus = "captured"
	PaymentStatusRefunded          PaymentStatus = "refunded"
	PaymentStatusPartiallyRefunded PaymentStatus = "partially_refunded"
)

type FulfillmentStatus string

const (
	FulfillmentStatusUnfulfilled FulfillmentStatus = "unfulfilled"
	FulfillmentStatusFulfilled   FulfillmentStatus = "fulfilled"
)

type Line struct {
	VariantID string
	SKU       string
	Quantity  int
	UnitPrice money.Amount
	Discount  money.Amount
	Tax       money.Amount
}

type Totals struct {
	Subtotal   money.Amount
	Discount   money.Amount
	Tax        money.Amount
	GrandTotal money.Amount
}

type Refund struct {
	ID        string
	Amount    money.Amount
	Reason    string
	CreatedAt time.Time
}

type Counter interface {
	Next(ctx interface{}, regionID string) (int64, error)
}

type Order struct {
	ID                string
	Number            string
	Status            Status
	PaymentStatus     PaymentStatus
	FulfillmentStatus FulfillmentStatus
	Lines             []Line
	Totals            Totals
	Refunds           []Refund
	RegionID          string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	totalsLocked      bool
}

func (o *Order) Recalculate() error {
	if o.totalsLocked {
		return ErrTotalsLocked
	}

	if len(o.Lines) == 0 {
		o.Totals = Totals{
			Subtotal:   money.NewAmount(0, o.RegionID),
			Discount:   money.NewAmount(0, o.RegionID),
			Tax:        money.NewAmount(0, o.RegionID),
			GrandTotal: money.NewAmount(0, o.RegionID),
		}
		return nil
	}

	currency := o.Lines[0].UnitPrice.Currency
	subtotal := money.NewAmount(0, currency)
	discount := money.NewAmount(0, currency)
	tax := money.NewAmount(0, currency)

	for _, line := range o.Lines {
		lineTotal, err := line.UnitPrice.Mul(int64(line.Quantity))
		if err != nil {
			return err
		}
		subtotal, err = subtotal.Add(lineTotal)
		if err != nil {
			return err
		}

		lineDiscount, err := line.Discount.Mul(int64(line.Quantity))
		if err != nil {
			return err
		}
		discount, err = discount.Add(lineDiscount)
		if err != nil {
			return err
		}

		lineTax, err := line.Tax.Mul(int64(line.Quantity))
		if err != nil {
			return err
		}
		tax, err = tax.Add(lineTax)
		if err != nil {
			return err
		}
	}

	grandTotal := subtotal
	grandTotal, _ = grandTotal.Add(tax)
	if discount.Value > 0 {
		grandTotal, _ = grandTotal.Sub(discount)
	}

	o.Totals = Totals{
		Subtotal:   money.NewAmount(subtotal.Value, currency),
		Discount:   money.NewAmount(discount.Value, currency),
		Tax:        money.NewAmount(tax.Value, currency),
		GrandTotal: money.NewAmount(grandTotal.Value, currency),
	}

	return nil
}

func (o *Order) Transition(to Status) error {
	if !isValidTransition(o.Status, to) {
		return ErrInvalidTransition
	}

	o.Status = to
	o.UpdatedAt = time.Now()

	if to == StatusCancelled {
		o.PaymentStatus = PaymentStatusPending
	}
	if to == StatusRefunded {
		o.PaymentStatus = PaymentStatusRefunded
	}

	return nil
}

func isValidTransition(from, to Status) bool {
	transitions := map[Status][]Status{
		StatusPending:   {StatusConfirmed, StatusCancelled},
		StatusConfirmed: {StatusFulfilled, StatusCancelled, StatusRefunded},
		StatusFulfilled: {StatusRefunded},
		StatusCancelled: {},
		StatusRefunded:  {},
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

func (o *Order) Refund(amount money.Amount, reason string) (Refund, error) {
	if o.PaymentStatus != PaymentStatusCaptured && o.PaymentStatus != PaymentStatusPartiallyRefunded {
		return Refund{}, ErrInvalidPaymentStatus
	}

	currency := o.Totals.GrandTotal.Currency
	totalRefunded := money.NewAmount(0, currency)
	for _, r := range o.Refunds {
		var err error
		totalRefunded, err = totalRefunded.Add(r.Amount)
		if err != nil {
			return Refund{}, err
		}
	}

	newTotalRefunded, err := totalRefunded.Add(amount)
	if err != nil {
		return Refund{}, err
	}

	if newTotalRefunded.Value > o.Totals.GrandTotal.Value {
		return Refund{}, ErrRefundExceedsCaptured
	}

	refund := Refund{
		ID:        generateRefundID(),
		Amount:    amount,
		Reason:    reason,
		CreatedAt: time.Now(),
	}

	o.Refunds = append(o.Refunds, refund)
	o.UpdatedAt = time.Now()

	if newTotalRefunded.Value == o.Totals.GrandTotal.Value {
		o.PaymentStatus = PaymentStatusRefunded
		o.Status = StatusRefunded
	} else {
		o.PaymentStatus = PaymentStatusPartiallyRefunded
	}

	return refund, nil
}

func (o *Order) CapturePayment() {
	o.PaymentStatus = PaymentStatusCaptured
	o.totalsLocked = true
	o.UpdatedAt = time.Now()
}

func generateRefundID() string {
	return "ref_" + time.Now().Format("20060102150405") + randomSuffix()
}

func randomSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[time.Now().Nanosecond()%len(charset)]
	}
	return string(b)
}

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusConfirmed, StatusFulfilled, StatusCancelled, StatusRefunded:
		return true
	default:
		return false
	}
}

func (ps PaymentStatus) IsValid() bool {
	switch ps {
	case PaymentStatusPending, PaymentStatusCaptured, PaymentStatusRefunded, PaymentStatusPartiallyRefunded:
		return true
	default:
		return false
	}
}

func (fs FulfillmentStatus) IsValid() bool {
	switch fs {
	case FulfillmentStatusUnfulfilled, FulfillmentStatusFulfilled:
		return true
	default:
		return false
	}
}
