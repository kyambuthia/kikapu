package routes

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

type CartHandler struct {
	store    cart.Store
	checkout *checkout.Service
}

func NewCartHandler(store cart.Store, checkoutSvc *checkout.Service) *CartHandler {
	return &CartHandler{
		store:    store,
		checkout: checkoutSvc,
	}
}

type CreateCartRequest struct {
	Currency string `json:"currency"`
}

type AddItemRequest struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
}

type UpdateItemRequest struct {
	Quantity int `json:"quantity"`
}

type CheckoutRequest struct {
	CartID string `json:"cart_id"`
}

type CheckoutResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

func (h *CartHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/carts", h.CreateCart)
	mux.HandleFunc("GET /v1/carts/{id}", h.GetCart)
	mux.HandleFunc("DELETE /v1/carts/{id}", h.DeleteCart)
	mux.HandleFunc("POST /v1/carts/{id}/items", h.AddItem)
	mux.HandleFunc("PATCH /v1/carts/{id}/items/{variantID}", h.UpdateItem)
	mux.HandleFunc("DELETE /v1/carts/{id}/items/{variantID}", h.RemoveItem)
	mux.HandleFunc("POST /v1/checkout", h.Checkout)
}

func (h *CartHandler) CreateCart(w http.ResponseWriter, r *http.Request) {
	var req CreateCartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	if req.Currency == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_currency", "currency is required", httpx.RequestID(r))
		return
	}

	id := uuid.New().String()
	c := cart.NewCart(id, req.Currency)

	if err := h.store.Save(c); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create cart", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *CartHandler) GetCart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	c, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get cart", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *CartHandler) DeleteCart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	c, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get cart", httpx.RequestID(r))
		return
	}

	c.Clear()
	if err := h.store.Save(c); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to clear cart", httpx.RequestID(r))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *CartHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req AddItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	c, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get cart", httpx.RequestID(r))
		return
	}

	version, err := c.AddItem(req.VariantID, req.Quantity, money.NewAmount(req.UnitPrice, c.Currency))
	if err != nil {
		if errors.Is(err, cart.ErrInvalidQuantity) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_quantity", err.Error(), httpx.RequestID(r))
			return
		}
		if errors.Is(err, cart.ErrCurrencyMismatch) {
			httpx.WriteError(w, http.StatusBadRequest, "currency_mismatch", err.Error(), httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to add item", httpx.RequestID(r))
		return
	}

	_ = version

	if err := h.store.Save(c); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save cart", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *CartHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	variantID := r.PathValue("variantID")

	var req UpdateItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	c, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get cart", httpx.RequestID(r))
		return
	}

	_, err = c.UpdateQuantity(variantID, req.Quantity)
	if err != nil {
		if errors.Is(err, cart.ErrInvalidQuantity) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_quantity", err.Error(), httpx.RequestID(r))
			return
		}
		if errors.Is(err, cart.ErrItemNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "item_not_found", err.Error(), httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update item", httpx.RequestID(r))
		return
	}

	if err := h.store.Save(c); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save cart", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *CartHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	variantID := r.PathValue("variantID")

	c, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get cart", httpx.RequestID(r))
		return
	}

	_, err = c.RemoveItem(variantID)
	if err != nil {
		if errors.Is(err, cart.ErrItemNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "item_not_found", err.Error(), httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to remove item", httpx.RequestID(r))
		return
	}

	if err := h.store.Save(c); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save cart", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *CartHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
		return
	}

	var req CheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	order, err := h.checkout.PlaceOrder(req.CartID, idempotencyKey)
	if err != nil {
		if errors.Is(err, cart.ErrCartNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "cart_not_found", "cart not found", httpx.RequestID(r))
			return
		}
		if errors.Is(err, checkout.ErrEmptyCart) {
			httpx.WriteError(w, http.StatusBadRequest, "empty_cart", err.Error(), httpx.RequestID(r))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to place order", httpx.RequestID(r))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CheckoutResponse{
		OrderID: order.ID,
		Status:  string(order.Status),
	})
}
