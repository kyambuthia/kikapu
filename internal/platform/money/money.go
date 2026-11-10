package money

import (
	"errors"
	"strconv"
)

var (
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrInvalidAmount    = errors.New("invalid amount")
)

type Amount struct {
	Value    int64
	Currency string
}

func NewAmount(value int64, currency string) Amount {
	return Amount{Value: value, Currency: currency}
}

func Parse(s string, currency string) (Amount, error) {
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return Amount{}, ErrInvalidAmount
	}
	return Amount{Value: value, Currency: currency}, nil
}

func (a Amount) Format() string {
	return strconv.FormatInt(a.Value, 10)
}

func (a Amount) String() string {
	return a.Format() + " " + a.Currency
}

func (a Amount) Add(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, ErrCurrencyMismatch
	}
	return Amount{Value: a.Value + other.Value, Currency: a.Currency}, nil
}

func (a Amount) Sub(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, ErrCurrencyMismatch
	}
	return Amount{Value: a.Value - other.Value, Currency: a.Currency}, nil
}

func (a Amount) Mul(multiplier int64) Amount {
	return Amount{Value: a.Value * multiplier, Currency: a.Currency}
}

func (a Amount) Div(divisor int64) Amount {
	return Amount{Value: a.Value / divisor, Currency: a.Currency}
}

func (a Amount) Round(decimalPlaces int) Amount {
	divisor := int64(1)
	for i := 0; i < decimalPlaces; i++ {
		divisor *= 10
	}
	rounded := (a.Value + divisor/2) / divisor
	return Amount{Value: rounded * divisor, Currency: a.Currency}
}
