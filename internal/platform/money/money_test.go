package money

import (
	"testing"
)

func TestAmountParseAndFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		currency string
		want     string
	}{
		{
			name:     "parse cents",
			input:    "100",
			currency: "USD",
			want:     "100",
		},
		{
			name:     "parse negative cents",
			input:    "-50",
			currency: "USD",
			want:     "-50",
		},
		{
			name:     "parse zero",
			input:    "0",
			currency: "USD",
			want:     "0",
		},
		{
			name:     "parse large value",
			input:    "999999999",
			currency: "JPY",
			want:     "999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, err := Parse(tt.input, tt.currency)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := amount.Format(); got != tt.want {
				t.Errorf("Format() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountAdd(t *testing.T) {
	tests := []struct {
		name    string
		a       Amount
		b       Amount
		want    Amount
		wantErr error
	}{
		{
			name: "add same currency",
			a:    NewAmount(100, "USD"),
			b:    NewAmount(50, "USD"),
			want: NewAmount(150, "USD"),
		},
		{
			name:    "add different currencies",
			a:       NewAmount(100, "USD"),
			b:       NewAmount(50, "EUR"),
			wantErr: ErrCurrencyMismatch,
		},
		{
			name: "add negative amount",
			a:    NewAmount(100, "USD"),
			b:    NewAmount(-30, "USD"),
			want: NewAmount(70, "USD"),
		},
		{
			name: "add with zero",
			a:    NewAmount(100, "USD"),
			b:    NewAmount(0, "USD"),
			want: NewAmount(100, "USD"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Add(tt.b)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Add() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Add() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Add() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountSub(t *testing.T) {
	tests := []struct {
		name    string
		a       Amount
		b       Amount
		want    Amount
		wantErr error
	}{
		{
			name: "subtract same currency",
			a:    NewAmount(100, "USD"),
			b:    NewAmount(30, "USD"),
			want: NewAmount(70, "USD"),
		},
		{
			name:    "subtract different currencies",
			a:       NewAmount(100, "USD"),
			b:       NewAmount(30, "EUR"),
			wantErr: ErrCurrencyMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Sub(tt.b)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Sub() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Sub() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Sub() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountMul(t *testing.T) {
	tests := []struct {
		name       string
		amount     Amount
		multiplier int64
		want       Amount
	}{
		{
			name:       "multiply by 2",
			amount:     NewAmount(100, "USD"),
			multiplier: 2,
			want:       NewAmount(200, "USD"),
		},
		{
			name:       "multiply by 0",
			amount:     NewAmount(100, "USD"),
			multiplier: 0,
			want:       NewAmount(0, "USD"),
		},
		{
			name:       "multiply by negative",
			amount:     NewAmount(100, "USD"),
			multiplier: -1,
			want:       NewAmount(-100, "USD"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.amount.Mul(tt.multiplier); got != tt.want {
				t.Errorf("Mul() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountRound(t *testing.T) {
	tests := []struct {
		name         string
		amount       Amount
		decimalPlaces int
		want         Amount
	}{
		{
			name:          "round to zero decimals rounds to nearest major unit 1",
			amount:        NewAmount(1050, "USD"),
			decimalPlaces: 0,
			want:          NewAmount(1000, "USD"),
		},
		{
			name:          "round to zero decimals rounds to nearest major unit 2",
			amount:        NewAmount(1550, "USD"),
			decimalPlaces: 0,
			want:          NewAmount(2000, "USD"),
		},
		{
			name:          "round to one decimal place",
			amount:        NewAmount(1234, "USD"),
			decimalPlaces: 1,
			want:          NewAmount(1230, "USD"),
		},
		{
			name:          "round to two decimal places",
			amount:        NewAmount(12345, "USD"),
			decimalPlaces: 2,
			want:          NewAmount(12300, "USD"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.amount.Round(tt.decimalPlaces); got != tt.want {
				t.Errorf("Round() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountParseInvalid(t *testing.T) {
	_, err := Parse("not-a-number", "USD")
	if err != ErrInvalidAmount {
		t.Errorf("Parse() error = %v, want %v", err, ErrInvalidAmount)
	}
}
