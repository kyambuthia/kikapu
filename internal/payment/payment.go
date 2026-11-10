package payment

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type State string

const (
	StatePending   State = "pending"
	StateAuthorized State = "authorized"
	StateCaptured  State = "captured"
	StateFailed    State = "failed"
	StateRefunded  State = "refunded"
)

var (
	ErrInvalidTransition = errors.New("invalid payment state transition")
	ErrChargeFailed      = errors.New("charge failed")
	ErrTimeout           = errors.New("payment timeout")
)

var validTransitions = map[State][]State{
	StatePending:   {StateAuthorized, StateFailed},
	StateAuthorized: {StateCaptured, StateRefunded, StateFailed},
	StateCaptured:  {StateRefunded},
	StateFailed:    {},
	StateRefunded:  {},
}

func (s State) CanTransitionTo(target State) bool {
	allowed, ok := validTransitions[s]
	if !ok {
		return false
	}
	for _, state := range allowed {
		if state == target {
			return true
		}
	}
	return false
}

type ChargeRequest struct {
	IDempotencyKey string
	Amount         money.Amount
	CustomerID     string
}

type ChargeResult struct {
	ID      string
	Status  State
	Message string
}

type Payment struct {
	ID        string
	State     State
	Amount    money.Amount
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Provider interface {
	Charge(req ChargeRequest) (ChargeResult, error)
	GetPayment(id string) (*Payment, error)
}

type FailureMode string

const (
	NoFailure       FailureMode = ""
	Declined        FailureMode = "declined"
	Timeout         FailureMode = "timeout"
	DuplicateCallback FailureMode = "duplicate-callback"
)

type FakeProvider struct {
	mu              sync.Mutex
	payments        map[string]*Payment
	idempotencyKeys map[string]string
	failureMode     FailureMode
	callCount       int
}

func NewFakeProvider(failureMode FailureMode) *FakeProvider {
	return &FakeProvider{
		payments:        make(map[string]*Payment),
		idempotencyKeys: make(map[string]string),
		failureMode:     failureMode,
	}
}

func (p *FakeProvider) Charge(req ChargeRequest) (ChargeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.callCount++

	if paymentID, ok := p.idempotencyKeys[req.IDempotencyKey]; ok {
		payment := p.payments[paymentID]
		return ChargeResult{
			ID:      paymentID,
			Status:  payment.State,
			Message: "duplicate",
		}, nil
	}

	if p.failureMode == Declined {
		payment := &Payment{
			ID:        fmt.Sprintf("pay_%d", time.Now().UnixNano()),
			State:     StateFailed,
			Amount:    req.Amount,
			Error:     "declined",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		p.payments[payment.ID] = payment
		return ChargeResult{}, ErrChargeFailed
	}

	if p.failureMode == Timeout {
		return ChargeResult{}, ErrTimeout
	}

	paymentID := fmt.Sprintf("pay_%d", time.Now().UnixNano())
	payment := &Payment{
		ID:        paymentID,
		State:     StateAuthorized,
		Amount:    req.Amount,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	p.payments[paymentID] = payment
	p.idempotencyKeys[req.IDempotencyKey] = paymentID

	return ChargeResult{
		ID:      paymentID,
		Status:  StateAuthorized,
		Message: "authorized",
	}, nil
}

func (p *FakeProvider) GetPayment(id string) (*Payment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	payment, ok := p.payments[id]
	if !ok {
		return nil, fmt.Errorf("payment not found: %s", id)
	}

	return payment, nil
}

func (p *FakeProvider) GetCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.callCount
}

func (p *FakeProvider) ResetCallCount() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount = 0
}

func (p *FakeProvider) SetFailureMode(mode FailureMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failureMode = mode
}
