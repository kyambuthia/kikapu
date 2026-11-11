package money

import (
	"math"
	"testing"
)

func TestAmountParseAndFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		currency string
		want     string
		wantErr  error
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
		{
			name:     "empty string",
			input:    "",
			currency: "USD",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "invalid characters",
			input:    "not-a-number",
			currency: "USD",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "max int64",
			input:    "9223372036854775807",
			currency: "USD",
			want:     "9223372036854775807",
		},
		{
			name:     "large negative value",
			input:    "-9223372036854775807",
			currency: "USD",
			want:     "-9223372036854775807",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, err := Parse(tt.input, tt.currency)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Parse() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
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
		{
			name:    "overflow positive",
			a:       NewAmount(math.MaxInt64, "USD"),
			b:       NewAmount(1, "USD"),
			wantErr: ErrOverflow,
		},
		{
			name:    "overflow negative",
			a:       NewAmount(math.MinInt64, "USD"),
			b:       NewAmount(-1, "USD"),
			wantErr: ErrOverflow,
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
		{
			name:    "overflow subtracting negative from max",
			a:       NewAmount(math.MaxInt64, "USD"),
			b:       NewAmount(-1, "USD"),
			wantErr: ErrOverflow,
		},
		{
			name:    "overflow subtracting positive from min",
			a:       NewAmount(math.MinInt64, "USD"),
			b:       NewAmount(1, "USD"),
			wantErr: ErrOverflow,
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
		wantErr    error
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
		{
			name:       "overflow max int64",
			amount:     NewAmount(math.MaxInt64/2+1, "USD"),
			multiplier: 2,
			wantErr:    ErrOverflow,
		},
		{
			name:       "overflow min int64",
			amount:     NewAmount(math.MinInt64, "USD"),
			multiplier: 2,
			wantErr:    ErrOverflow,
		},
		{
			name:       "min int64 by itself",
			amount:     NewAmount(math.MinInt64, "USD"),
			multiplier: math.MinInt64,
			wantErr:    ErrOverflow,
		},
		{
			name:       "large positive values",
			amount:     NewAmount(math.MaxInt64/(1<<31), "USD"),
			multiplier: (1 << 31) + 1,
			wantErr:    ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.amount.Mul(tt.multiplier)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Mul() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Mul() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Mul() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountDiv(t *testing.T) {
	tests := []struct {
		name    string
		amount  Amount
		divisor int64
		want    Amount
		wantErr error
	}{
		{
			name:    "divide by 2",
			amount:  NewAmount(100, "USD"),
			divisor: 2,
			want:    NewAmount(50, "USD"),
		},
		{
			name:    "divide by itself",
			amount:  NewAmount(100, "USD"),
			divisor: 100,
			want:    NewAmount(1, "USD"),
		},
		{
			name:    "division by zero",
			amount:  NewAmount(100, "USD"),
			divisor: 0,
			wantErr: ErrDivisionByZero,
		},
		{
			name:    "min int64 by -1",
			amount:  NewAmount(math.MinInt64, "USD"),
			divisor: -1,
			wantErr: ErrOverflow,
		},
		{
			name:    "divide negative",
			amount:  NewAmount(-100, "USD"),
			divisor: 2,
			want:    NewAmount(-50, "USD"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.amount.Div(tt.divisor)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Div() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Div() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Div() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountRoundHalfEven(t *testing.T) {
	tests := []struct {
		name    string
		amount  Amount
		step    int64
		want    Amount
		wantErr error
	}{
		{
			name:   "round 2.5 to 2 (bankers rounding)",
			amount: NewAmount(25, "USD"),
			step:   10,
			want:   NewAmount(20, "USD"),
		},
		{
			name:   "round 3.5 to 4 (bankers rounding)",
			amount: NewAmount(35, "USD"),
			step:   10,
			want:   NewAmount(40, "USD"),
		},
		{
			name:   "round up on > half",
			amount: NewAmount(26, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:   "round down on < half",
			amount: NewAmount(24, "USD"),
			step:   10,
			want:   NewAmount(20, "USD"),
		},
		{
			name:   "exact division",
			amount: NewAmount(30, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:   "negative value round up",
			amount: NewAmount(-25, "USD"),
			step:   10,
			want:   NewAmount(-20, "USD"),
		},
		{
			name:   "negative value round down",
			amount: NewAmount(-35, "USD"),
			step:   10,
			want:   NewAmount(-40, "USD"),
		},
		{
			name:    "invalid step zero",
			amount:  NewAmount(100, "USD"),
			step:    0,
			wantErr: ErrInvalidRounding,
		},
		{
			name:    "invalid step negative",
			amount:  NewAmount(100, "USD"),
			step:    -10,
			wantErr: ErrInvalidRounding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.amount.RoundHalfEven(tt.step)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("RoundHalfEven() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("RoundHalfEven() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("RoundHalfEven() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountRoundHalfUp(t *testing.T) {
	tests := []struct {
		name    string
		amount  Amount
		step    int64
		want    Amount
		wantErr error
	}{
		{
			name:   "round 2.5 up to 3 (standard rounding)",
			amount: NewAmount(25, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:   "round 3.5 up to 4",
			amount: NewAmount(35, "USD"),
			step:   10,
			want:   NewAmount(40, "USD"),
		},
		{
			name:   "round up on > half",
			amount: NewAmount(26, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:   "round down on < half",
			amount: NewAmount(24, "USD"),
			step:   10,
			want:   NewAmount(20, "USD"),
		},
		{
			name:   "exact value",
			amount: NewAmount(30, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:   "negative -2.5 rounds to -3",
			amount: NewAmount(-25, "USD"),
			step:   10,
			want:   NewAmount(-30, "USD"),
		},
		{
			name:    "invalid step zero",
			amount:  NewAmount(100, "USD"),
			step:    0,
			wantErr: ErrInvalidRounding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.amount.RoundHalfUp(tt.step)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("RoundHalfUp() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("RoundHalfUp() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("RoundHalfUp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountRoundDown(t *testing.T) {
	tests := []struct {
		name    string
		amount  Amount
		step    int64
		want    Amount
		wantErr error
	}{
		{
			name:   "round down positive",
			amount: NewAmount(27, "USD"),
			step:   10,
			want:   NewAmount(20, "USD"),
		},
		{
			name:   "round down negative",
			amount: NewAmount(-27, "USD"),
			step:   10,
			want:   NewAmount(-20, "USD"),
		},
		{
			name:   "exact multiple",
			amount: NewAmount(30, "USD"),
			step:   10,
			want:   NewAmount(30, "USD"),
		},
		{
			name:    "invalid step zero",
			amount:  NewAmount(100, "USD"),
			step:    0,
			wantErr: ErrInvalidRounding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.amount.RoundDown(tt.step)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("RoundDown() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("RoundDown() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("RoundDown() = %v, want %v", got, tt.want)
			}
		})
	}
}
