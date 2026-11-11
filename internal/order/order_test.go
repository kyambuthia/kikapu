package order

import (
	"sync"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr error
	}{
		{"pending to confirmed", StatusPending, StatusConfirmed, nil},
		{"pending to cancelled", StatusPending, StatusCancelled, nil},
		{"confirmed to fulfilled", StatusConfirmed, StatusFulfilled, nil},
		{"confirmed to cancelled", StatusConfirmed, StatusCancelled, nil},
		{"confirmed to refunded", StatusConfirmed, StatusRefunded, nil},
		{"fulfilled to refunded", StatusFulfilled, StatusRefunded, nil},
		{"pending to fulfilled", StatusPending, StatusFulfilled, ErrInvalidTransition},
		{"pending to refunded", StatusPending, StatusRefunded, ErrInvalidTransition},
		{"cancelled to confirmed", StatusCancelled, StatusConfirmed, ErrInvalidTransition},
		{"refunded to pending", StatusRefunded, StatusPending, ErrInvalidTransition},
		{"fulfilled to confirmed", StatusFulfilled, StatusConfirmed, ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{Status: tt.from}
			err := order.Transition(tt.to)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Transition() error = %v, wantErr %v", err, tt.wantErr)
				}
				if order.Status != tt.from {
					t.Errorf("Transition() status changed from %v to %v", tt.from, order.Status)
				}
			} else {
				if err != nil {
					t.Errorf("Transition() unexpected error = %v", err)
				}
				if order.Status != tt.to {
					t.Errorf("Transition() status = %v, want %v", order.Status, tt.to)
				}
			}
		})
	}
}

func TestTotalsCalculation(t *testing.T) {
	tests := []struct {
		name           string
		lines          []Line
		wantSubtotal   int64
		wantDiscount   int64
		wantTax        int64
		wantGrandTotal int64
		currency       string
	}{
		{
			name: "single line no discount or tax",
			lines: []Line{
				{
					VariantID: "var001",
					SKU:       "SKU-001",
					Quantity:  2,
					UnitPrice: money.NewAmount(1000, "USD"),
					Discount:  money.NewAmount(0, "USD"),
					Tax:       money.NewAmount(0, "USD"),
				},
			},
			wantSubtotal:   2000,
			wantDiscount:   0,
			wantTax:        0,
			wantGrandTotal: 2000,
			currency:       "USD",
		},
		{
			name: "single line with discount",
			lines: []Line{
				{
					VariantID: "var001",
					SKU:       "SKU-001",
					Quantity:  2,
					UnitPrice: money.NewAmount(1000, "USD"),
					Discount:  money.NewAmount(100, "USD"),
					Tax:       money.NewAmount(0, "USD"),
				},
			},
			wantSubtotal:   2000,
			wantDiscount:   200,
			wantTax:        0,
			wantGrandTotal: 1800,
			currency:       "USD",
		},
		{
			name: "single line with tax",
			lines: []Line{
				{
					VariantID: "var001",
					SKU:       "SKU-001",
					Quantity:  2,
					UnitPrice: money.NewAmount(1000, "USD"),
					Discount:  money.NewAmount(0, "USD"),
					Tax:       money.NewAmount(160, "USD"),
				},
			},
			wantSubtotal:   2000,
			wantDiscount:   0,
			wantTax:        320,
			wantGrandTotal: 2320,
			currency:       "USD",
		},
		{
			name: "single line with discount and tax",
			lines: []Line{
				{
					VariantID: "var001",
					SKU:       "SKU-001",
					Quantity:  2,
					UnitPrice: money.NewAmount(1000, "USD"),
					Discount:  money.NewAmount(100, "USD"),
					Tax:       money.NewAmount(160, "USD"),
				},
			},
			wantSubtotal:   2000,
			wantDiscount:   200,
			wantTax:        320,
			wantGrandTotal: 2120,
			currency:       "USD",
		},
		{
			name: "multiple lines",
			lines: []Line{
				{
					VariantID: "var001",
					SKU:       "SKU-001",
					Quantity:  2,
					UnitPrice: money.NewAmount(1000, "USD"),
					Discount:  money.NewAmount(50, "USD"),
					Tax:       money.NewAmount(100, "USD"),
				},
				{
					VariantID: "var002",
					SKU:       "SKU-002",
					Quantity:  1,
					UnitPrice: money.NewAmount(5000, "USD"),
					Discount:  money.NewAmount(0, "USD"),
					Tax:       money.NewAmount(800, "USD"),
				},
			},
			wantSubtotal:   7000,
			wantDiscount:   100,
			wantTax:        1000,
			wantGrandTotal: 7900,
			currency:       "USD",
		},
		{
			name:           "empty lines",
			lines:          []Line{},
			wantSubtotal:   0,
			wantDiscount:   0,
			wantTax:        0,
			wantGrandTotal: 0,
			currency:       "USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{
				Lines:    tt.lines,
				RegionID: tt.currency,
			}

			err := order.Recalculate()
			if err != nil {
				t.Fatalf("Recalculate() error = %v", err)
			}

			if order.Totals.Subtotal.Value != tt.wantSubtotal {
				t.Errorf("Subtotal = %d, want %d", order.Totals.Subtotal.Value, tt.wantSubtotal)
			}
			if order.Totals.Discount.Value != tt.wantDiscount {
				t.Errorf("Discount = %d, want %d", order.Totals.Discount.Value, tt.wantDiscount)
			}
			if order.Totals.Tax.Value != tt.wantTax {
				t.Errorf("Tax = %d, want %d", order.Totals.Tax.Value, tt.wantTax)
			}
			if order.Totals.GrandTotal.Value != tt.wantGrandTotal {
				t.Errorf("GrandTotal = %d, want %d", order.Totals.GrandTotal.Value, tt.wantGrandTotal)
			}
		})
	}
}

func TestTotalsLocked(t *testing.T) {
	order := &Order{
		Lines: []Line{
			{
				VariantID: "var001",
				SKU:       "SKU-001",
				Quantity:  2,
				UnitPrice: money.NewAmount(1000, "USD"),
				Discount:  money.NewAmount(0, "USD"),
				Tax:       money.NewAmount(0, "USD"),
			},
		},
		RegionID: "USD",
	}

	err := order.Recalculate()
	if err != nil {
		t.Fatalf("first Recalculate() error = %v", err)
	}

	order.CapturePayment()

	order.Lines[0].Quantity = 5
	err = order.Recalculate()
	if err != ErrTotalsLocked {
		t.Errorf("Recalculate() error = %v, want %v", err, ErrTotalsLocked)
	}
}

func TestRefund(t *testing.T) {
	tests := []struct {
		name            string
		paymentStatus   PaymentStatus
		grandTotal      int64
		refunds         []Refund
		refundAmount    int64
		currency        string
		wantErr         error
		wantStatus      PaymentStatus
		wantOrderStatus Status
	}{
		{
			name:            "partial refund on captured payment",
			paymentStatus:   PaymentStatusCaptured,
			grandTotal:      10000,
			refunds:         []Refund{},
			refundAmount:    3000,
			currency:        "USD",
			wantErr:         nil,
			wantStatus:      PaymentStatusPartiallyRefunded,
			wantOrderStatus: StatusConfirmed,
		},
		{
			name:            "full refund",
			paymentStatus:   PaymentStatusCaptured,
			grandTotal:      10000,
			refunds:         []Refund{},
			refundAmount:    10000,
			currency:        "USD",
			wantErr:         nil,
			wantStatus:      PaymentStatusRefunded,
			wantOrderStatus: StatusRefunded,
		},
		{
			name:          "two partial refunds equal total",
			paymentStatus: PaymentStatusPartiallyRefunded,
			grandTotal:    10000,
			refunds: []Refund{
				{Amount: money.NewAmount(5000, "USD")},
			},
			refundAmount:    5000,
			currency:        "USD",
			wantErr:         nil,
			wantStatus:      PaymentStatusRefunded,
			wantOrderStatus: StatusRefunded,
		},
		{
			name:          "refund exceeds captured",
			paymentStatus: PaymentStatusCaptured,
			grandTotal:    10000,
			refunds: []Refund{
				{Amount: money.NewAmount(8000, "USD")},
			},
			refundAmount:    3000,
			currency:        "USD",
			wantErr:         ErrRefundExceedsCaptured,
			wantStatus:      PaymentStatusCaptured,
			wantOrderStatus: StatusConfirmed,
		},
		{
			name:            "refund on pending payment",
			paymentStatus:   PaymentStatusPending,
			grandTotal:      10000,
			refunds:         []Refund{},
			refundAmount:    5000,
			currency:        "USD",
			wantErr:         ErrInvalidPaymentStatus,
			wantStatus:      PaymentStatusPending,
			wantOrderStatus: StatusConfirmed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{
				Status:        StatusConfirmed,
				PaymentStatus: tt.paymentStatus,
				Totals: Totals{
					GrandTotal: money.NewAmount(tt.grandTotal, tt.currency),
				},
				Refunds: tt.refunds,
			}

			refund, err := order.Refund(money.NewAmount(tt.refundAmount, tt.currency), "test refund")

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Refund() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Refund() unexpected error = %v", err)
			}

			if refund.Amount.Value != tt.refundAmount {
				t.Errorf("Refund amount = %d, want %d", refund.Amount.Value, tt.refundAmount)
			}

			if order.PaymentStatus != tt.wantStatus {
				t.Errorf("Payment status = %v, want %v", order.PaymentStatus, tt.wantStatus)
			}

			if order.Status != tt.wantOrderStatus {
				t.Errorf("Order status = %v, want %v", order.Status, tt.wantOrderStatus)
			}
		})
	}
}

func TestRefundTwice(t *testing.T) {
	order := &Order{
		Status:        StatusConfirmed,
		PaymentStatus: PaymentStatusCaptured,
		Totals: Totals{
			GrandTotal: money.NewAmount(10000, "USD"),
		},
		Refunds: []Refund{},
	}

	_, err := order.Refund(money.NewAmount(3000, "USD"), "first refund")
	if err != nil {
		t.Fatalf("first refund error = %v", err)
	}

	if order.PaymentStatus != PaymentStatusPartiallyRefunded {
		t.Errorf("Payment status = %v, want %v", order.PaymentStatus, PaymentStatusPartiallyRefunded)
	}

	_, err = order.Refund(money.NewAmount(3000, "USD"), "second refund")
	if err != nil {
		t.Fatalf("second refund error = %v", err)
	}

	if len(order.Refunds) != 2 {
		t.Errorf("Number of refunds = %d, want 2", len(order.Refunds))
	}

	_, err = order.Refund(money.NewAmount(5000, "USD"), "third refund")
	if err != ErrRefundExceedsCaptured {
		t.Errorf("third refund error = %v, want %v", err, ErrRefundExceedsCaptured)
	}
}

func TestCounterMonotonicity(t *testing.T) {
	counter := NewInMemoryCounter()
	regionID := "reg-001"

	var wg sync.WaitGroup
	results := make(chan int64, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := counter.Next(nil, regionID)
			if err != nil {
				t.Errorf("Next() error = %v", err)
				return
			}
			results <- seq
		}()
	}

	wg.Wait()
	close(results)

	seen := make(map[int64]bool)
	for seq := range results {
		if seen[seq] {
			t.Errorf("Duplicate sequence number = %d", seq)
		}
		seen[seq] = true
	}

	for i := int64(1); i <= 100; i++ {
		if !seen[i] {
			t.Errorf("Missing sequence number = %d", i)
		}
	}
}

func TestOrderNumberFormat(t *testing.T) {
	tests := []struct {
		seq  int64
		want string
	}{
		{1, "KIK-000001"},
		{123, "KIK-000123"},
		{999999, "KIK-999999"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatOrderNumber(tt.seq)
			if got != tt.want {
				t.Errorf("FormatOrderNumber(%d) = %s, want %s", tt.seq, got, tt.want)
			}
		})
	}
}

func TestStatusValidation(t *testing.T) {
	tests := []struct {
		status Status
		valid  bool
	}{
		{StatusPending, true},
		{StatusConfirmed, true},
		{StatusFulfilled, true},
		{StatusCancelled, true},
		{StatusRefunded, true},
		{Status("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.valid {
				t.Errorf("Status(%s).IsValid() = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestPaymentStatusValidation(t *testing.T) {
	tests := []struct {
		status PaymentStatus
		valid  bool
	}{
		{PaymentStatusPending, true},
		{PaymentStatusCaptured, true},
		{PaymentStatusRefunded, true},
		{PaymentStatusPartiallyRefunded, true},
		{PaymentStatus("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.valid {
				t.Errorf("PaymentStatus(%s).IsValid() = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestFulfillmentStatusValidation(t *testing.T) {
	tests := []struct {
		status FulfillmentStatus
		valid  bool
	}{
		{FulfillmentStatusUnfulfilled, true},
		{FulfillmentStatusFulfilled, true},
		{FulfillmentStatus("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.valid {
				t.Errorf("FulfillmentStatus(%s).IsValid() = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestRefundGeneratedID(t *testing.T) {
	order := &Order{
		Status:        StatusConfirmed,
		PaymentStatus: PaymentStatusCaptured,
		Totals: Totals{
			GrandTotal: money.NewAmount(10000, "USD"),
		},
		Refunds: []Refund{},
	}

	refund, err := order.Refund(money.NewAmount(1000, "USD"), "test")
	if err != nil {
		t.Fatalf("Refund() error = %v", err)
	}

	if refund.ID == "" {
		t.Error("Refund ID is empty")
	}

	if refund.CreatedAt.IsZero() {
		t.Error("Refund CreatedAt is zero")
	}
}

func TestStore(t *testing.T) {
	store := NewStore()

	order := &Order{
		ID:        "ord-001",
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := store.Save(order)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Get("ord-001")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != order.ID {
		t.Errorf("Get() ID = %s, want %s", got.ID, order.ID)
	}

	_, err = store.Get("nonexistent")
	if err != ErrOrderNotFound {
		t.Errorf("Get() error = %v, want %v", err, ErrOrderNotFound)
	}
}
