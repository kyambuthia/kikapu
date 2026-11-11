package order

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type Store struct {
	mu     sync.RWMutex
	orders map[string]*Order
}

func NewStore() *Store {
	return &Store{
		orders: make(map[string]*Order),
	}
}

func (s *Store) Get(id string) (*Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[id]
	if !ok {
		return nil, ErrOrderNotFound
	}

	orderCopy := *order
	return &orderCopy, nil
}

func (s *Store) Save(order *Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	orderCopy := *order
	s.orders[order.ID] = &orderCopy
	return nil
}

var ErrOrderNotFound = errors.New("order not found")

type Handler struct {
	store   *Store
	counter Counter
}

func NewHandler(store *Store, counter Counter) *Handler {
	return &Handler{
		store:   store,
		counter: counter,
	}
}

type OrderResponse struct {
	ID                string            `json:"id"`
	Number            string            `json:"number"`
	Status            Status            `json:"status"`
	PaymentStatus     PaymentStatus     `json:"payment_status"`
	FulfillmentStatus FulfillmentStatus `json:"fulfillment_status"`
	Lines             []LineResponse    `json:"lines"`
	Totals            TotalsResponse    `json:"totals"`
	Refunds           []RefundResponse  `json:"refunds,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type LineResponse struct {
	VariantID string        `json:"variant_id"`
	SKU       string        `json:"sku"`
	Quantity  int           `json:"quantity"`
	UnitPrice MoneyResponse `json:"unit_price"`
	Discount  MoneyResponse `json:"discount"`
	Tax       MoneyResponse `json:"tax"`
}

type TotalsResponse struct {
	Subtotal   MoneyResponse `json:"subtotal"`
	Discount   MoneyResponse `json:"discount"`
	Tax        MoneyResponse `json:"tax"`
	GrandTotal MoneyResponse `json:"grand_total"`
}

type MoneyResponse struct {
	Value    int64  `json:"value"`
	Currency string `json:"currency"`
}

type RefundResponse struct {
	ID        string        `json:"id"`
	Amount    MoneyResponse `json:"amount"`
	Reason    string        `json:"reason"`
	CreatedAt time.Time     `json:"created_at"`
}

type RefundRequest struct {
	Amount MoneyRequest `json:"amount"`
	Reason string       `json:"reason"`
}

type MoneyRequest struct {
	Value    int64  `json:"value"`
	Currency string `json:"currency"`
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_order_id", "order ID is required", httpx.RequestID(r))
		return
	}

	order, err := h.store.Get(id)
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.WriteError(w, http.StatusNotFound, "order_not_found", "order not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to retrieve order", httpx.RequestID(r))
		return
	}

	resp := orderToResponse(order)
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) RefundOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_order_id", "order ID is required", httpx.RequestID(r))
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	var req RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body", httpx.RequestID(r))
		return
	}

	order, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "order_not_found", "order not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to retrieve order", httpx.RequestID(r))
		return
	}

	amount := money.NewAmount(req.Amount.Value, req.Amount.Currency)
	refund, err := order.Refund(amount, req.Reason)
	if err != nil {
		switch err {
		case ErrInvalidPaymentStatus:
			httpx.WriteError(w, http.StatusBadRequest, "invalid_payment_status", "order cannot be refunded in current payment status", httpx.RequestID(r))
		case ErrRefundExceedsCaptured:
			httpx.WriteError(w, http.StatusBadRequest, "refund_exceeds_captured", "refund amount exceeds captured amount", httpx.RequestID(r))
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to process refund", httpx.RequestID(r))
		}
		return
	}

	if err := h.store.Save(order); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save order", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, refundToResponse(refund))
}

func orderToResponse(o *Order) OrderResponse {
	lines := make([]LineResponse, len(o.Lines))
	for i, line := range o.Lines {
		lines[i] = LineResponse{
			VariantID: line.VariantID,
			SKU:       line.SKU,
			Quantity:  line.Quantity,
			UnitPrice: MoneyResponse{Value: line.UnitPrice.Value, Currency: line.UnitPrice.Currency},
			Discount:  MoneyResponse{Value: line.Discount.Value, Currency: line.Discount.Currency},
			Tax:       MoneyResponse{Value: line.Tax.Value, Currency: line.Tax.Currency},
		}
	}

	refunds := make([]RefundResponse, len(o.Refunds))
	for i, r := range o.Refunds {
		refunds[i] = RefundResponse{
			ID:        r.ID,
			Amount:    MoneyResponse{Value: r.Amount.Value, Currency: r.Amount.Currency},
			Reason:    r.Reason,
			CreatedAt: r.CreatedAt,
		}
	}

	return OrderResponse{
		ID:                o.ID,
		Number:            o.Number,
		Status:            o.Status,
		PaymentStatus:     o.PaymentStatus,
		FulfillmentStatus: o.FulfillmentStatus,
		Lines:             lines,
		Totals: TotalsResponse{
			Subtotal:   MoneyResponse{Value: o.Totals.Subtotal.Value, Currency: o.Totals.Subtotal.Currency},
			Discount:   MoneyResponse{Value: o.Totals.Discount.Value, Currency: o.Totals.Discount.Currency},
			Tax:        MoneyResponse{Value: o.Totals.Tax.Value, Currency: o.Totals.Tax.Currency},
			GrandTotal: MoneyResponse{Value: o.Totals.GrandTotal.Value, Currency: o.Totals.GrandTotal.Currency},
		},
		Refunds:   refunds,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}
}

func refundToResponse(r Refund) RefundResponse {
	return RefundResponse{
		ID:        r.ID,
		Amount:    MoneyResponse{Value: r.Amount.Value, Currency: r.Amount.Currency},
		Reason:    r.Reason,
		CreatedAt: r.CreatedAt,
	}
}
