package paystack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
)

const (
	defaultBaseURL = "https://api.paystack.co"
)

var (
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrChargeFailed       = errors.New("charge failed")
	ErrVerificationFailed = errors.New("verification failed")
	ErrUnauthorized       = errors.New("unauthorized")
)

type Config struct {
	BaseURL    string
	SecretKey  string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client
}

func New(cfg Config) (*Client, error) {
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("%w: secret key is required", ErrInvalidConfig)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("%w: invalid base URL: %v", ErrInvalidConfig, err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &Client{
		baseURL:    baseURL,
		secretKey:  cfg.SecretKey,
		httpClient: httpClient,
	}, nil
}

type initializeRequest struct {
	Email     string `json:"email"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Reference string `json:"reference"`
}

type initializeResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AuthorizationURL string `json:"authorization_url"`
		AccessCode       string `json:"access_code"`
		Reference        string `json:"reference"`
	} `json:"data"`
}

type verifyResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		ID            int64  `json:"id"`
		Status        string `json:"status"`
		Reference     string `json:"reference"`
		Amount        int64  `json:"amount"`
		Currency      string `json:"currency"`
		PaidAt        string `json:"paid_at"`
		CreatedAt     string `json:"created_at"`
		Channel       string `json:"channel"`
		Authorization struct {
			AuthorizationCode string `json:"authorization_code"`
		} `json:"authorization"`
	} `json:"data"`
}

type ChargeResult struct {
	Reference        string
	Status           payment.State
	AuthorizationURL string
}

func (c *Client) Charge(req payment.ChargeRequest) (payment.ChargeResult, error) {
	ctx := context.Background()
	initReq := initializeRequest{
		Email:     req.CustomerID,
		Amount:    req.Amount.Value,
		Currency:  req.Amount.Currency,
		Reference: req.IDempotencyKey,
	}

	body, err := json.Marshal(initReq)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/transaction/initialize", bytes.NewReader(body))
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.secretKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return payment.ChargeResult{}, fmt.Errorf("%w: status %d, message: %s", ErrChargeFailed, resp.StatusCode, string(respBody))
	}

	var initResp initializeResponse
	if err := json.Unmarshal(respBody, &initResp); err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if !initResp.Status {
		return payment.ChargeResult{}, fmt.Errorf("%w: %s", ErrChargeFailed, initResp.Message)
	}

	return payment.ChargeResult{
		ID:      initResp.Data.Reference,
		Status:  payment.StatePending,
		Message: initResp.Message,
	}, nil
}

func (c *Client) Verify(reference string) (payment.ChargeResult, error) {
	ctx := context.Background()
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/transaction/verify/"+reference, nil)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.secretKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return payment.ChargeResult{}, ErrUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		return payment.ChargeResult{}, fmt.Errorf("%w: status %d", ErrVerificationFailed, resp.StatusCode)
	}

	var verifyResp verifyResponse
	if err := json.Unmarshal(respBody, &verifyResp); err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if !verifyResp.Status {
		return payment.ChargeResult{}, fmt.Errorf("%w: %s", ErrVerificationFailed, verifyResp.Message)
	}

	state := mapPaystackStatus(verifyResp.Data.Status)

	return payment.ChargeResult{
		ID:      verifyResp.Data.Reference,
		Status:  state,
		Message: verifyResp.Message,
	}, nil
}

func mapPaystackStatus(status string) payment.State {
	switch status {
	case "success":
		return payment.StateCaptured
	case "failed":
		return payment.StateFailed
	case "abandoned":
		return payment.StatePending
	default:
		return payment.StatePending
	}
}

func (c *Client) GetPayment(id string) (*payment.Payment, error) {
	return nil, fmt.Errorf("GetPayment not supported for Paystack provider, use Verify instead")
}
