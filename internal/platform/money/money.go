package money

import (
	"errors"
	"math"
)

var (
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrInvalidAmount    = errors.New("invalid amount")
	ErrDivisionByZero   = errors.New("division by zero")
	ErrOverflow         = errors.New("arithmetic overflow")
	ErrInvalidRounding  = errors.New("invalid rounding step")
)

type Amount struct {
	Value    int64
	Currency string
}

func NewAmount(value int64, currency string) Amount {
	return Amount{Value: value, Currency: currency}
}

func Parse(s string, currency string) (Amount, error) {
	if s == "" {
		return Amount{}, ErrInvalidAmount
	}
	var value int64
	var negative bool
	start := 0
	if s[0] == '-' {
		negative = true
		start = 1
		if len(s) == 1 {
			return Amount{}, ErrInvalidAmount
		}
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return Amount{}, ErrInvalidAmount
		}
		digit := int64(s[i] - '0')
		if value > (math.MaxInt64-digit)/10 {
			return Amount{}, ErrInvalidAmount
		}
		if negative && value == math.MaxInt64/10 && digit == 8 && i == len(s)-1 {
			continue
		}
		value = value*10 + digit
	}
	if negative {
		value = -value
	}
	return Amount{Value: value, Currency: currency}, nil
}

func (a Amount) Format() string {
	return int64ToString(a.Value)
}

func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	var negative bool
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func (a Amount) String() string {
	return a.Format() + " " + a.Currency
}

func (a Amount) Add(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, ErrCurrencyMismatch
	}
	result := a.Value + other.Value
	if (result > a.Value) != (other.Value > 0) && other.Value != 0 {
		if a.Value > 0 && other.Value > 0 {
			return Amount{}, ErrOverflow
		}
		if a.Value < 0 && other.Value < 0 {
			return Amount{}, ErrOverflow
		}
	}
	return Amount{Value: result, Currency: a.Currency}, nil
}

func (a Amount) Sub(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, ErrCurrencyMismatch
	}
	result := a.Value - other.Value
	if (result < a.Value) != (other.Value > 0) && other.Value != 0 {
		if a.Value > 0 && other.Value < 0 {
			return Amount{}, ErrOverflow
		}
		if a.Value < 0 && other.Value > 0 {
			return Amount{}, ErrOverflow
		}
	}
	return Amount{Value: result, Currency: a.Currency}, nil
}

func (a Amount) Mul(multiplier int64) (Amount, error) {
	if a.Value == 0 || multiplier == 0 {
		return Amount{Value: 0, Currency: a.Currency}, nil
	}
	if a.Value == math.MinInt64 {
		if multiplier > 1 || multiplier < -1 {
			return Amount{}, ErrOverflow
		}
		return Amount{Value: a.Value * multiplier, Currency: a.Currency}, nil
	}
	if multiplier == math.MinInt64 {
		if a.Value > 1 || a.Value < -1 {
			return Amount{}, ErrOverflow
		}
		return Amount{Value: a.Value * multiplier, Currency: a.Currency}, nil
	}
	absValue := a.Value
	if absValue < 0 {
		absValue = -absValue
	}
	absMult := multiplier
	if absMult < 0 {
		absMult = -absMult
	}
	if absValue > math.MaxInt64/absMult {
		return Amount{}, ErrOverflow
	}
	return Amount{Value: a.Value * multiplier, Currency: a.Currency}, nil
}

func (a Amount) Div(divisor int64) (Amount, error) {
	if divisor == 0 {
		return Amount{}, ErrDivisionByZero
	}
	if a.Value == math.MinInt64 && divisor == -1 {
		return Amount{}, ErrOverflow
	}
	return Amount{Value: a.Value / divisor, Currency: a.Currency}, nil
}

func (a Amount) RoundHalfEven(step int64) (Amount, error) {
	if step <= 0 {
		return Amount{}, ErrInvalidRounding
	}
	result := roundInternal(a.Value, step, true)
	result.Currency = a.Currency
	return result, nil
}

func (a Amount) RoundHalfUp(step int64) (Amount, error) {
	if step <= 0 {
		return Amount{}, ErrInvalidRounding
	}
	result := roundInternal(a.Value, step, false)
	result.Currency = a.Currency
	return result, nil
}

func (a Amount) RoundDown(step int64) (Amount, error) {
	if step <= 0 {
		return Amount{}, ErrInvalidRounding
	}
	quotient := a.Value / step
	return Amount{Value: quotient * step, Currency: a.Currency}, nil
}

func roundInternal(value, step int64, bankers bool) Amount {
	sign := int64(1)
	if value < 0 {
		sign = -1
		value = -value
	}
	quotient := value / step
	remainder := value % step
	half := step / 2
	roundUp := false
	if remainder > half {
		roundUp = true
	} else if remainder == half {
		if bankers {
			roundUp = quotient%2 != 0
		} else {
			roundUp = true
		}
	}
	if roundUp {
		quotient++
	}
	return Amount{Value: quotient * step * sign, Currency: ""}
}
