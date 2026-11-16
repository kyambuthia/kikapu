package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyambuthia/kikapu/internal/cart"
	"github.com/kyambuthia/kikapu/internal/catalog"
	"github.com/kyambuthia/kikapu/internal/checkout"
	"github.com/kyambuthia/kikapu/internal/inventory"
	"github.com/kyambuthia/kikapu/internal/outbox"
	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
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

	catalogRepo := catalog.NewInMemoryRepository([]catalog.Product{
		{
			ID:     "prod001",
			Slug:   "wireless-mouse",
			Name:   "Wireless Mouse",
			Status: "published",
			Variants: []catalog.Variant{
				{ID: "var001", SKU: "WM-BLK-001", Price: money.NewAmount(2999, "USD"), Currency: "USD"},
				{ID: "var002", SKU: "WM-WHT-001", Price: money.NewAmount(3499, "USD"), Currency: "USD"},
			},
		},
	})

	return NewCartHandler(cartStore, checkoutSvc, catalogRepo)
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

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2}
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

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2}
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

	addBody := AddItemRequest{VariantID: "var001", Quantity: 2}
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

func TestCartHandler_AddItem_UsesServerPriceAndRejectsUnknownVariant(t *testing.T) {
	handler := setupCartHandler()

	createBody := CreateCartRequest{Currency: "USD"}
	jsonBody, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/v1/carts", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateCart(w, req)

	var c cart.Cart
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("Failed to unmarshal cart: %v", err)
	}

	// Client attempts to send unknown unit_price field; strict JSON decoding rejects it.
	spoofPayload := []byte(`{"variant_id":"var001","quantity":2,"unit_price":1}`)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(spoofPayload))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddItem(unknown unit_price field) status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	// Trailing garbage after JSON must also be rejected.
	garbagePayload := []byte(`{"variant_id":"var001","quantity":2}garbage`)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(garbagePayload))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddItem(trailing garbage) status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	// Valid payload resolves canonical 2999 price from catalog.
	validPayload := []byte(`{"variant_id":"var001","quantity":2}`)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(validPayload))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddItem() status = %d, want %d", w.Code, http.StatusOK)
	}

	var updated cart.Cart
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("Failed to unmarshal updated cart: %v", err)
	}
	if len(updated.Lines) != 1 || updated.Lines[0].UnitPrice.Value != 2999 {
		t.Fatalf("UnitPrice = %+v, want canonical 2999", updated.Lines)
	}

	// Unknown variant must return 404.
	missingPayload := []byte(`{"variant_id":"var999","quantity":1}`)
	req = httptest.NewRequest("POST", "/v1/carts/"+c.ID+"/items", bytes.NewReader(missingPayload))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", c.ID)
	w = httptest.NewRecorder()
	handler.AddItem(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("AddItem(unknown variant) status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCartHandler_ValidatesCurrencyAndCheckoutCartID(t *testing.T) {
	handler := setupCartHandler()

	// Lowercase / non-ISO currency must return 400 instead of hitting DB CHECK constraint.
	req := httptest.NewRequest(http.MethodPost, "/v1/carts", bytes.NewReader([]byte(`{"currency":"usd"}`)))
	w := httptest.NewRecorder()
	handler.CreateCart(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateCart(usd) status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	// Missing cart_id on checkout must return 400.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/checkout", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set("Idempotency-Key", "idem-missing-cart")
	w2 := httptest.NewRecorder()
	handler.Checkout(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("Checkout(empty cart_id) status = %d, want %d", w2.Code, http.StatusBadRequest)
	}
}
