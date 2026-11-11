package mpesa

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

var (
	ErrInvalidPayload  = errors.New("invalid payload")
	ErrForbiddenSource = errors.New("forbidden source IP")
	ErrNotAllowed      = errors.New("IP allowlist not configured")
)

type CallbackMetadata struct {
	Item []CallbackItem `json:"Item"`
}

type CallbackItem struct {
	Name  string      `json:"Name"`
	Value interface{} `json:"Value"`
}

type STKCallback struct {
	MerchantRequestID string            `json:"MerchantRequestID"`
	CheckoutRequestID string            `json:"CheckoutRequestID"`
	ResultCode        int               `json:"ResultCode"`
	ResultDesc        string            `json:"ResultDesc"`
	CallbackMetadata  *CallbackMetadata `json:"CallbackMetadata"`
}

type CallbackPayload struct {
	Body struct {
		STKCallback STKCallback `json:"stkCallback"`
	} `json:"Body"`
}

type ProcessedCallback struct {
	CheckoutRequestID string
	MerchantRequestID string
	ResultCode        int
	ResultDesc        string
	Amount            int64
	MpesaReceipt      string
	PhoneNumber       string
	TransactionDate   int64
	State             payment.State
}

type CallbackStore interface {
	GetCallback(checkoutRequestID string, resultCode int) (*ProcessedCallback, bool)
	SaveCallback(cb *ProcessedCallback) error
}

type InMemoryCallbackStore struct {
	mu        sync.RWMutex
	callbacks map[string]*ProcessedCallback
}

func NewInMemoryCallbackStore() *InMemoryCallbackStore {
	return &InMemoryCallbackStore{
		callbacks: make(map[string]*ProcessedCallback),
	}
}

func (s *InMemoryCallbackStore) GetCallback(checkoutRequestID string, resultCode int) (*ProcessedCallback, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := callbackKey(checkoutRequestID, resultCode)
	cb, ok := s.callbacks[key]
	return cb, ok
}

func (s *InMemoryCallbackStore) SaveCallback(cb *ProcessedCallback) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := callbackKey(cb.CheckoutRequestID, cb.ResultCode)
	s.callbacks[key] = cb
	return nil
}

func callbackKey(checkoutRequestID string, resultCode int) string {
	return checkoutRequestID + "_" + intToString(resultCode)
}

type CallbackHandler struct {
	store        CallbackStore
	allowedCIDRs []string
	networks     []*net.IPNet
	logger       *slog.Logger
	production   bool
}

func NewCallbackHandler(store CallbackStore, allowedCIDRs []string, logger *slog.Logger, production bool) (*CallbackHandler, error) {
	networks := make([]*net.IPNet, 0, len(allowedCIDRs))
	for _, cidr := range allowedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			if ip := net.ParseIP(cidr); ip != nil {
				if ip.To4() != nil {
					cidr = cidr + "/32"
				} else {
					cidr = cidr + "/128"
				}
				_, network, err = net.ParseCIDR(cidr)
				if err != nil {
					return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
				}
			} else {
				return nil, fmt.Errorf("invalid IP or CIDR %s: %w", cidr, err)
			}
		}
		networks = append(networks, network)
	}

	return &CallbackHandler{
		store:        store,
		allowedCIDRs: allowedCIDRs,
		networks:     networks,
		logger:       logger,
		production:   production,
	}, nil
}

func (h *CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := httpx.RequestID(r)

	if h.production && len(h.networks) == 0 {
		h.logger.Error("IP allowlist not configured in production mode",
			"request_id", requestID,
		)
		httpx.WriteError(w, http.StatusForbidden, "ip_allowlist_required", "IP allowlist must be configured in production mode", requestID)
		return
	}

	if len(h.networks) > 0 {
		ip, err := parseRemoteAddr(r)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_ip", "failed to parse source IP", requestID)
			return
		}

		if !h.ipAllowed(ip) {
			h.logger.Warn("callback from disallowed IP",
				"ip", ip.String(),
				"request_id", requestID,
			)
			httpx.WriteError(w, http.StatusForbidden, "forbidden_source", "source IP not in allowlist", requestID)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_error", "failed to read request body", requestID)
		return
	}
	defer r.Body.Close()

	var payload CallbackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_payload", "failed to parse callback payload", requestID)
		return
	}

	cb := payload.Body.STKCallback

	if cb.CheckoutRequestID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_checkout_request_id", "CheckoutRequestID is required", requestID)
		return
	}

	if _, ok := h.store.GetCallback(cb.CheckoutRequestID, cb.ResultCode); ok {
		h.logger.Info("duplicate callback ignored",
			"checkout_request_id", cb.CheckoutRequestID,
			"result_code", cb.ResultCode,
			"request_id", requestID,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	processed, err := h.processCallback(&cb, requestID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error(), err.Error(), requestID)
		return
	}

	if err := h.store.SaveCallback(processed); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "save_error", "failed to save callback", requestID)
		return
	}

	h.logger.Info("callback processed",
		"checkout_request_id", processed.CheckoutRequestID,
		"merchant_request_id", processed.MerchantRequestID,
		"result_code", processed.ResultCode,
		"state", processed.State,
		"amount", processed.Amount,
		"receipt", processed.MpesaReceipt,
		"request_id", requestID,
	)

	w.WriteHeader(http.StatusOK)
}

func (h *CallbackHandler) processCallback(cb *STKCallback, requestID string) (*ProcessedCallback, error) {
	processed := &ProcessedCallback{
		CheckoutRequestID: cb.CheckoutRequestID,
		MerchantRequestID: cb.MerchantRequestID,
		ResultCode:        cb.ResultCode,
		ResultDesc:        cb.ResultDesc,
		State:             mapResultCodeFromInt(cb.ResultCode),
	}

	if cb.CallbackMetadata != nil {
		for _, item := range cb.CallbackMetadata.Item {
			switch item.Name {
			case "Amount":
				if v, ok := item.Value.(float64); ok {
					processed.Amount = int64(v)
				}
			case "MpesaReceiptNumber":
				if v, ok := item.Value.(string); ok {
					processed.MpesaReceipt = v
				}
			case "PhoneNumber":
				if v, ok := item.Value.(float64); ok {
					processed.PhoneNumber = fmt.Sprintf("%.0f", v)
				}
			case "TransactionDate":
				if v, ok := item.Value.(float64); ok {
					processed.TransactionDate = int64(v)
				}
			}
		}
	}

	return processed, nil
}

func (h *CallbackHandler) ipAllowed(ip net.IP) bool {
	for _, network := range h.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteAddr(r *http.Request) (net.IP, error) {
	addr := r.Header.Get("X-Forwarded-For")
	if addr != "" {
		parts := strings.Split(addr, ",")
		if len(parts) > 0 {
			addr = strings.TrimSpace(parts[0])
		}
	}
	if addr == "" {
		addr = r.RemoteAddr
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	return ip, nil
}

func mapResultCodeFromInt(code int) payment.State {
	switch code {
	case 0:
		return payment.StateCaptured
	case 1032, 1037:
		return payment.StateFailed
	default:
		return payment.StateFailed
	}
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var negative bool
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
