package region

import (
	"errors"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidID       = errors.New("region: invalid ID")
	ErrInvalidName     = errors.New("region: invalid name")
	ErrInvalidCurrency = errors.New("region: invalid currency")
	ErrInvalidCountry  = errors.New("region: invalid country code")
	ErrInvalidTaxRate  = errors.New("region: invalid tax rate")
)

type Region struct {
	ID           string
	Name         string
	Currency     string
	Countries    []string
	TaxRateBPS   int
	TaxInclusive bool
}

func (r Region) Validate() error {
	if r.ID == "" {
		return ErrInvalidID
	}
	if r.Name == "" {
		return ErrInvalidName
	}
	if !money.IsISO4217(r.Currency) {
		return ErrInvalidCurrency
	}
	for _, c := range r.Countries {
		if !isISO3166Alpha2(c) {
			return ErrInvalidCountry
		}
	}
	if r.TaxRateBPS < 0 || r.TaxRateBPS > 10000 {
		return ErrInvalidTaxRate
	}
	return nil
}

func isISO3166Alpha2(code string) bool {
	if len(code) != 2 {
		return false
	}
	return (code[0] >= 'A' && code[0] <= 'Z') && (code[1] >= 'A' && code[1] <= 'Z')
}

type Repository interface {
	Get(ctx interface{}, id string) (*Region, error)
	List(ctx interface{}) ([]Region, error)
}
