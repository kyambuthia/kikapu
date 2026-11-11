package fulfillment

import (
	"encoding/json"
	"net/http"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type CreateFulfillmentRequest struct {
	Items []Item `json:"items"`
}

type UpdateStatusRequest struct {
	Status Status `json:"status"`
}

type AddTrackingRequest struct {
	Carrier string `json:"carrier"`
	Number  string `json:"number"`
	URL     string `json:"url"`
}

type Handlers struct {
	service *Service
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/admin/orders/", h.handleOrderFulfillments)
	mux.HandleFunc("/v1/admin/fulfillments/", h.handleFulfillment)
}

func (h *Handlers) handleOrderFulfillments(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if len(path) < len("/v1/admin/orders/") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "invalid path", httpx.RequestID(r))
		return
	}

	rest := path[len("/v1/admin/orders/"):]
	parts := splitPath(rest)
	if len(parts) < 2 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "invalid path", httpx.RequestID(r))
		return
	}

	orderID := parts[0]
	action := parts[1]

	if action != "fulfillments" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found", httpx.RequestID(r))
		return
	}

	if r.Method == http.MethodPost && len(parts) == 2 {
		h.create(w, r, orderID)
		return
	}

	if r.Method == http.MethodGet && len(parts) == 2 {
		h.listByOrder(w, r, orderID)
		return
	}

	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
}

func (h *Handlers) handleFulfillment(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if len(path) < len("/v1/admin/fulfillments/") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "invalid path", httpx.RequestID(r))
		return
	}

	rest := path[len("/v1/admin/fulfillments/"):]
	parts := splitPath(rest)
	if len(parts) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "fulfillment id required", httpx.RequestID(r))
		return
	}

	fulfillmentID := parts[0]

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			h.get(w, r, fulfillmentID)
			return
		}
		if r.Method == http.MethodPatch {
			h.updateStatus(w, r, fulfillmentID)
			return
		}
	}

	if len(parts) == 2 && parts[1] == "tracking" {
		if r.Method == http.MethodPost {
			h.addTracking(w, r, fulfillmentID)
			return
		}
	}

	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", httpx.RequestID(r))
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range splitPathHelper(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitPathHelper(path string) []string {
	var result []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if start < i {
				result = append(result, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		result = append(result, path[start:])
	}
	return result
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request, orderID string) {
	var req CreateFulfillmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	if len(req.Items) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "validation_error", "at least one item is required", httpx.RequestID(r))
		return
	}

	fulfillment, err := h.service.Create(r.Context(), orderID, req.Items)
	if err == ErrOrderNotConfirmed {
		httpx.WriteError(w, http.StatusConflict, "order_not_confirmed", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrOverFulfillment {
		httpx.WriteError(w, http.StatusBadRequest, "over_fulfillment", err.Error(), httpx.RequestID(r))
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create fulfillment", httpx.RequestID(r))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(fulfillment)
}

func (h *Handlers) updateStatus(w http.ResponseWriter, r *http.Request, fulfillmentID string) {
	var req UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	if !req.Status.IsValid() {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status", "invalid fulfillment status", httpx.RequestID(r))
		return
	}

	err := h.service.UpdateStatus(r.Context(), fulfillmentID, req.Status)
	if err == ErrInvalidTransition {
		httpx.WriteError(w, http.StatusConflict, "invalid_transition", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrFulfillmentNotFound {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error(), httpx.RequestID(r))
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update status", httpx.RequestID(r))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) addTracking(w http.ResponseWriter, r *http.Request, fulfillmentID string) {
	var req AddTrackingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", httpx.RequestID(r))
		return
	}

	tracking := Tracking{
		Carrier: req.Carrier,
		Number:  req.Number,
		URL:     req.URL,
	}

	err := h.service.AddTracking(r.Context(), fulfillmentID, tracking)
	if err == ErrMissingCarrier {
		httpx.WriteError(w, http.StatusBadRequest, "missing_carrier", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrMissingTrackingNumber {
		httpx.WriteError(w, http.StatusBadRequest, "missing_tracking_number", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrInvalidTrackingURL {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_tracking_url", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrFulfillmentNotFound {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error(), httpx.RequestID(r))
		return
	}
	if err == ErrInvalidTransition {
		httpx.WriteError(w, http.StatusConflict, "invalid_transition", "tracking can only be added to shipped fulfillments", httpx.RequestID(r))
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to add tracking", httpx.RequestID(r))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request, fulfillmentID string) {
	fulfillment, err := h.service.repo.Get(fulfillmentID)
	if err == ErrFulfillmentNotFound {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error(), httpx.RequestID(r))
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get fulfillment", httpx.RequestID(r))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fulfillment)
}

func (h *Handlers) listByOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	fulfillments, err := h.service.repo.ListByOrder(orderID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list fulfillments", httpx.RequestID(r))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": fulfillments,
	})
}
