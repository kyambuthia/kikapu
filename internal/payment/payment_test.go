package payment

import (
	"testing"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestState_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from State
		to   State
		want bool
	}{
		{StatePending, StateAuthorized, true},
		{StatePending, StateFailed, true},
		{StatePending, StateCaptured, false},
		{StateAuthorized, StateCaptured, true},
		{StateAuthorized, StateRefunded, true},
		{StateAuthorized, StateFailed, true},
		{StateCaptured, StateRefunded, true},
		{StateCaptured, StateFailed, false},
		{StateFailed, StatePending, false},
		{StateFailed, StateAuthorized, false},
		{StateRefunded, StateCaptured, false},
		{StateRefunded, StatePending, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("State.CanTransitionTo(%s -> %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestFakeProvider_Charge(t *testing.T) {
	tests := []struct {
		name        string
		failureMode FailureMode
		wantErr     error
		wantStatus  State
	}{
		{
			name:        "successful charge",
			failureMode: NoFailure,
			wantErr:     nil,
			wantStatus:  StateAuthorized,
		},
		{
			name:        "declined charge",
			failureMode: Declined,
			wantErr:     ErrChargeFailed,
		},
		{
			name:        "timeout",
			failureMode: Timeout,
			wantErr:     ErrTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewFakeProvider(tt.failureMode)

			result, err := provider.Charge(ChargeRequest{
				IDempotencyKey: "key001",
				Amount:         money.NewAmount(1000, "USD"),
				CustomerID:     "cust001",
			})

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Charge() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Charge() unexpected error: %v", err)
				return
			}

			if result.Status != tt.wantStatus {
				t.Errorf("Charge() status = %v, want %v", result.Status, tt.wantStatus)
			}
		})
	}
}

func TestFakeProvider_Idempotency(t *testing.T) {
	provider := NewFakeProvider(NoFailure)

	req := ChargeRequest{
		IDempotencyKey: "key001",
		Amount:         money.NewAmount(1000, "USD"),
		CustomerID:     "cust001",
	}

	result1, err := provider.Charge(req)
	if err != nil {
		t.Fatalf("Charge() error: %v", err)
	}

	provider.ResetCallCount()

	result2, err := provider.Charge(req)
	if err != nil {
		t.Fatalf("Charge() error: %v", err)
	}

	if result1.ID != result2.ID {
		t.Errorf("Charge() ID should be the same for idempotency key, got %s and %s", result1.ID, result2.ID)
	}

	if provider.GetCallCount() != 1 {
		t.Errorf("Charge() call count = %d, want 1", provider.GetCallCount())
	}
}

func TestFakeProvider_IllegalTransition(t *testing.T) {
	provider := NewFakeProvider(NoFailure)

	_, err := provider.Charge(ChargeRequest{
		IDempotencyKey: "key001",
		Amount:         money.NewAmount(1000, "USD"),
		CustomerID:     "cust001",
	})
	if err != nil {
		t.Fatalf("Charge() error: %v", err)
	}

	illegalTransitions := []struct {
		from State
		to   State
	}{
		{StatePending, StateCaptured},
		{StateCaptured, StateFailed},
		{StateFailed, StateAuthorized},
		{StateRefunded, StateCaptured},
	}

	for _, tt := range illegalTransitions {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if tt.from.CanTransitionTo(tt.to) {
				t.Errorf("Illegal transition allowed: %s -> %s", tt.from, tt.to)
			}
		})
	}
}
