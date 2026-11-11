package tax

import (
	"errors"

	"github.com/kyambuthia/kikapu/internal/order"
	"github.com/kyambuthia/kikapu/internal/platform/money"
	"github.com/kyambuthia/kikapu/internal/region"
)

var (
	ErrEmptyLines       = errors.New("tax: empty lines")
	ErrCurrencyMismatch = errors.New("tax: currency mismatch")
)

func Rate(r region.Region) int {
	return r.TaxRateBPS
}

func TaxFor(lines []order.Line, bps int, inclusive bool) ([]money.Amount, money.Amount, error) {
	if len(lines) == 0 {
		return nil, money.Amount{}, ErrEmptyLines
	}

	currency := lines[0].UnitPrice.Currency

	type taxCalc struct {
		numerator   int64
		denominator int64
	}

	perLineTax := make([]taxCalc, len(lines))
	var totalNumerator int64 = 0
	var totalDenominator int64 = 0

	for i, line := range lines {
		if line.UnitPrice.Currency != currency {
			return nil, money.Amount{}, ErrCurrencyMismatch
		}

		lineTotal, err := line.UnitPrice.Mul(int64(line.Quantity))
		if err != nil {
			return nil, money.Amount{}, err
		}

		var numerator int64
		var denominator int64
		if inclusive {
			numerator = lineTotal.Value * int64(bps)
			denominator = 10000 + int64(bps)
		} else {
			numerator = lineTotal.Value * int64(bps)
			denominator = 10000
		}

		perLineTax[i] = taxCalc{numerator: numerator, denominator: denominator}
		totalNumerator += numerator

		if i == 0 {
			totalDenominator = denominator
		}
	}

	perLineRounded := make([]money.Amount, len(lines))
	sumRounded := int64(0)

	for i, tc := range perLineTax {
		rounded := roundHalfEvenInt(tc.numerator, tc.denominator)
		perLineRounded[i] = money.NewAmount(rounded, currency)
		sumRounded += rounded
	}

	expectedTotal := roundHalfEvenInt(totalNumerator, totalDenominator)

	if sumRounded != expectedTotal {
		residual := expectedTotal - sumRounded
		maxIdx := 0
		for i := 1; i < len(perLineRounded); i++ {
			if perLineRounded[i].Value > perLineRounded[maxIdx].Value {
				maxIdx = i
			}
		}
		perLineRounded[maxIdx] = money.NewAmount(perLineRounded[maxIdx].Value+residual, currency)
	}

	return perLineRounded, money.NewAmount(expectedTotal, currency), nil
}

func roundHalfEvenInt(numerator, denominator int64) int64 {
	if denominator == 0 {
		return 0
	}

	intPart := numerator / denominator
	remainder := numerator % denominator

	if remainder < 0 {
		remainder = -remainder
	}

	half := denominator / 2

	if remainder > half {
		if numerator >= 0 {
			return intPart + 1
		}
		return intPart - 1
	} else if remainder == half {
		if intPart%2 != 0 {
			if numerator >= 0 {
				return intPart + 1
			}
			return intPart - 1
		}
	}

	return intPart
}
