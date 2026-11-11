package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
)

func setupCartHandler() *CartHandler {
	cartStore := cart.NewInMemoryStore()
	inventoryStore := inventory.NewMemoryStore(nil)
	paymentProvider := payment.NewFakeProvider(payment.NoFailure)
	idempotencyStore := checkout.NewIdempotencyStore()
	eventOutbox := outbox.NewInMemoryOutbox()
	checkoutSvc := checkout.NewService(cartStore, inventoryStore, paymentProvider, idempotencyStore, eventOutbox)

	inventoryStore.SetLevel("var001", "loc1", 100)
	inventoryStore.SetLevel("var002", "loc1", 100)

	return NewCartHandler(cartStore, checkoutSvc)
}

func TestCartHandler_CreateCart(t *testing.T) {
	handler := setupCartHandler()

	body := CreateCartRequest{Currency: "USD"}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/carts", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateCart(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("CreateCart() status = %d, want %d", w.Code, http.StatusCreated)
	}

	var c cart.Cart
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if c.Currency != "USD" {
		t.Errorf("CreateCart() currency = %s, want USD", c.Currency)
	}
}

func TestCartHandler_AddItem(t *testing.T) {
	handler := setupCartHandler()

	createBody := CreateCartRequest{Currency: "USD"}
	jsonBody, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/v1/carts", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateCart(w, req)

	var c cart.Cart
	json.Unmarshal(w.Body.Bytes(), &c)

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2, UnitPrice: 1000}
	jsonBody, _ = json.Marshal(addBody)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("AddItem() status = %d, want %d", w.Code, http.StatusOK)
	}

	var updated cart.Cart
	json.Unmarshal(w.Body.Bytes(), &updated)

	if len(updated.Lines) != 1 {
		t.Errorf("AddItem() lines = %d, want 1", len(updated.Lines))
	}

	if updated.Lines[0].Quantity != 2 {
		t.Errorf("AddItem() quantity = %d, want 2", updated.Lines[0].Quantity)
	}
}

func TestCartHandler_Checkout(t *testing.T) {
	handler := setupCartHandler()

	createBody := CreateCartRequest{Currency: "USD"}
	jsonBody, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/v1/carts", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateCart(w, req)

	var c cart.Cart
	json.Unmarshal(w.Body.Bytes(), &c)

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2, UnitPrice: 1000}
	jsonBody, _ = json.Marshal(addBody)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)

	checkoutBody := CheckoutRequest{CartID: c.ID}
	jsonBody, _ = json.Marshal(checkoutBody)
	req = httptest.NewRequest("POST", "/v1/checkout", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "test-key-001")
	w = httptest.NewRecorder()
	handler.Checkout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Checkout() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp CheckoutResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.OrderID == "" {
		t.Error("Checkout() order_id should not be empty")
	}

	if resp.Status != "paid" {
		t.Errorf("Checkout() status = %s, want paid", resp.Status)
	}
}

func TestCartHandler_Checkout_MissingIdempotencyKey(t *testing.T) {
	handler := setupCartHandler()

	checkoutBody := CheckoutRequest{CartID: "cart001"}
	jsonBody, _ := json.Marshal(checkoutBody)
	req := httptest.NewRequest("POST", "/v1/checkout", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Checkout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Checkout() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCartHandler_Checkout_Idempotency(t *testing.T) {
	handler := setupCartHandler()

	createBody := CreateCartRequest{Currency: "USD"}
	jsonBody, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/v1/carts", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateCart(w, req)

	var c cart.Cart
	json.Unmarshal(w.Body.Bytes(), &c)

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2, UnitPrice: 1000}
	jsonBody, _ = json.Marshal(addBody)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)

	checkoutBody := CheckoutRequest{CartID: c.ID}
	jsonBody, _ = json.Marshal(checkoutBody)

	req = httptest.NewRequest("POST", "/v1/checkout", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-key-001")
	w = httptest.NewRecorder()
	handler.Checkout(w, req)

	var resp1 CheckoutResponse
	json.Unmarshal(w.Body.Bytes(), &resp1)

	req = httptest.NewRequest("POST", "/v1/checkout", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-key-001")
	w = httptest.NewRecorder()
	handler.Checkout(w, req)

	var resp2 CheckoutResponse
	json.Unmarshal(w.Body.Bytes(), &resp2)

	if resp1.OrderID != resp2.OrderID {
		t.Errorf("Idempotency failed: got different order IDs %s and %s", resp1.OrderID, resp2.OrderID)
	}
}
