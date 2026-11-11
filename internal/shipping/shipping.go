package shipping

import (
	"errors"
	"sort"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrNoShippingZone   = errors.New("no shipping zone for destination")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrNegativeAmount   = errors.New("negative amount not allowed")
	ErrInvalidWeight    = errors.New("invalid weight")
	ErrZoneNotFound     = errors.New("zone not found")
)

type Zone struct {
	ID        string
	Name      string
	Countries []string
}

type RateKind string

const (
	KindFlat     RateKind = "flat"
	KindWeight   RateKind = "weight"
	KindFreeOver RateKind = "free_over"
)

type Rate struct {
	ID         string
	ZoneID     string
	Name       string
	Kind       RateKind
	Amount     money.Amount
	MinWeightG int
	FreeOver   money.Amount
}

type Applicable struct {
	RateID string
	Name   string
	Amount money.Amount
	Reason string
}

func (z Zone) MatchesCountry(countryCode string) bool {
	for _, c := range z.Countries {
		if c == countryCode {
			return true
		}
	}
	return false
}

type ZoneMatcher struct {
	zones []Zone
}

func NewZoneMatcher(zones []Zone) *ZoneMatcher {
	return &ZoneMatcher{zones: zones}
}

func (zm *ZoneMatcher) FindZone(countryCode string) (Zone, error) {
	for _, z := range zm.zones {
		if z.MatchesCountry(countryCode) {
			return z, nil
		}
	}
	return Zone{}, ErrNoShippingZone
}

func cartWeight(c *cart.Cart) int {
	totalWeight := 0
	for _, line := range c.Lines {
		totalWeight += line.Quantity * 1000
	}
	return totalWeight
}

func subtotal(c *cart.Cart) (money.Amount, error) {
	return c.Total()
}

func RatesFor(zone Zone, rates []Rate, cart *cart.Cart, cartsSubtotal money.Amount, currency string) ([]Applicable, error) {
	var applicable []Applicable

	for _, rate := range rates {
		if rate.ZoneID != zone.ID {
			continue
		}

		app, ok, err := evaluateRate(rate, cart, cartsSubtotal, currency)
		if err != nil {
			return nil, err
		}
		if ok {
			applicable = append(applicable, app)
		}
	}

	sortApplicable(applicable)

	return applicable, nil
}

func evaluateRate(rate Rate, cart *cart.Cart, subtotal money.Amount, currency string) (Applicable, bool, error) {
	switch rate.Kind {
	case KindFreeOver:
		if subtotal.Currency != currency {
			return Applicable{}, false, ErrCurrencyMismatch
		}
		if subtotal.Value >= rate.FreeOver.Value {
			return Applicable{
				RateID: rate.ID,
				Name:   rate.Name,
				Amount: money.NewAmount(0, currency),
				Reason: "free_over",
			}, true, nil
		}
		return Applicable{}, false, nil

	case KindFlat:
		if rate.Amount.Currency != currency {
			return Applicable{}, false, ErrCurrencyMismatch
		}
		return Applicable{
			RateID: rate.ID,
			Name:   rate.Name,
			Amount: rate.Amount,
			Reason: "flat",
		}, true, nil

	case KindWeight:
		if rate.Amount.Currency != currency {
			return Applicable{}, false, ErrCurrencyMismatch
		}
		weight := cartWeight(cart)
		if weight >= rate.MinWeightG {
			return Applicable{
				RateID: rate.ID,
				Name:   rate.Name,
				Amount: rate.Amount,
				Reason: "weight",
			}, true, nil
		}
		return Applicable{}, false, nil

	default:
		return Applicable{}, false, nil
	}
}

func sortApplicable(rates []Applicable) {
	sort.Slice(rates, func(i, j int) bool {
		if rates[i].Reason == "free_over" && rates[j].Reason != "free_over" {
			return true
		}
		if rates[j].Reason == "free_over" && rates[i].Reason != "free_over" {
			return false
		}

		if rates[i].Amount.Value != rates[j].Amount.Value {
			return rates[i].Amount.Value < rates[j].Amount.Value
		}

		return rates[i].RateID < rates[j].RateID
	})
}
