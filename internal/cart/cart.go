package cart

import (
	"errors"
	"fmt"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/money"
)

var (
	ErrInvalidQuantity  = errors.New("quantity must be between 1 and 99")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrCartNotFound     = errors.New("cart not found")
	ErrVersionMismatch  = errors.New("cart version mismatch")
	ErrItemNotFound     = errors.New("item not found in cart")
)

type Line struct {
	VariantID string       `json:"variant_id"`
	Quantity  int          `json:"quantity"`
	UnitPrice money.Amount `json:"unit_price"`
}

type Cart struct {
	ID        string    `json:"id"`
	Currency  string    `json:"currency"`
	Lines     []Line    `json:"lines"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewCart(id, currency string) *Cart {
	return &Cart{
		ID:        id,
		Currency:  currency,
		Lines:     []Line{},
		Version:   1,
		UpdatedAt: time.Now(),
	}
}

func (c *Cart) AddItem(variantID string, quantity int, unitPrice money.Amount) (int, error) {
	if quantity < 1 || quantity > 99 {
		return 0, ErrInvalidQuantity
	}

	if unitPrice.Currency != c.Currency {
		return 0, ErrCurrencyMismatch
	}

	for i, line := range c.Lines {
		if line.VariantID == variantID {
			newQty := line.Quantity + quantity
			if newQty > 99 {
				return 0, ErrInvalidQuantity
			}
			c.Lines[i].Quantity = newQty
			c.Version++
			c.UpdatedAt = time.Now()
			return c.Version, nil
		}
	}

	c.Lines = append(c.Lines, Line{
		VariantID: variantID,
		Quantity:  quantity,
		UnitPrice: unitPrice,
	})
	c.Version++
	c.UpdatedAt = time.Now()
	return c.Version, nil
}

func (c *Cart) UpdateQuantity(variantID string, quantity int) (int, error) {
	if quantity < 1 || quantity > 99 {
		return 0, ErrInvalidQuantity
	}

	for i, line := range c.Lines {
		if line.VariantID == variantID {
			c.Lines[i].Quantity = quantity
			c.Version++
			c.UpdatedAt = time.Now()
			return c.Version, nil
		}
	}

	return 0, ErrItemNotFound
}

func (c *Cart) RemoveItem(variantID string) (int, error) {
	for i, line := range c.Lines {
		if line.VariantID == variantID {
			c.Lines = append(c.Lines[:i], c.Lines[i+1:]...)
			c.Version++
			c.UpdatedAt = time.Now()
			return c.Version, nil
		}
	}
	return 0, ErrItemNotFound
}

func (c *Cart) Clear() int {
	c.Lines = []Line{}
	c.Version++
	c.UpdatedAt = time.Now()
	return c.Version
}

func (c *Cart) Total() (money.Amount, error) {
	if len(c.Lines) == 0 {
		return money.NewAmount(0, c.Currency), nil
	}

	total := money.NewAmount(0, c.Currency)
	for _, line := range c.Lines {
		lineTotal, err := line.UnitPrice.Mul(int64(line.Quantity))
		if err != nil {
			return money.Amount{}, fmt.Errorf("cart: calculate line total: %w", err)
		}
		total, err = total.Add(lineTotal)
		if err != nil {
			return money.Amount{}, err
		}
	}
	return total, nil
}

type Store interface {
	Get(id string) (*Cart, error)
	Save(cart *Cart) error
}
