// Package mpesa implements a payment provider for Safaricom M-Pesa Daraja API.
//
// M-Pesa amounts are expressed in integer KES (Kenyan Shillings) with NO minor units
// (no cents). For example, an amount of 100 represents 100 KES, not 1.00 KES.
// This is a Safaricom M-Pesa API requirement.
package mpesa

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
)

const (
	defaultBaseURL = "https://sandbox.safaricom.co.ke"
)

var (
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrChargeFailed       = errors.New("charge failed")
	ErrVerificationFailed = errors.New("verification failed")
	ErrUnauthorized       = errors.New("unauthorized")
)

type Config struct {
	BaseURL       string
	ConsumerKey   string
	ConsumerSecret string
	ShortCode     string
	Passkey       string
	CallbackURL   string
	HTTPClient    *http.Client
}

type Client struct {
	baseURL        string
	consumerKey    string
	consumerSecret string
	shortCode      string
	passkey        string
	callbackURL    string
	httpClient     *http.Client

	tokenMu       sync.RWMutex
	accessToken   string
	tokenExpiry   time.Time
}

func New(cfg Config) (*Client, error) {
	if cfg.ConsumerKey == "" {
		return nil, fmt.Errorf("%w: consumer key is required", ErrInvalidConfig)
	}
	if cfg.ConsumerSecret == "" {
		return nil, fmt.Errorf("%w: consumer secret is required", ErrInvalidConfig)
	}
	if cfg.ShortCode == "" {
		return nil, fmt.Errorf("%w: short code is required", ErrInvalidConfig)
	}
	if cfg.Passkey == "" {
		return nil, fmt.Errorf("%w: passkey is required", ErrInvalidConfig)
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
		baseURL:        baseURL,
		consumerKey:    cfg.ConsumerKey,
		consumerSecret: cfg.ConsumerSecret,
		shortCode:      cfg.ShortCode,
		passkey:        cfg.Passkey,
		callbackURL:    cfg.CallbackURL,
		httpClient:     httpClient,
	}, nil
}

type oauthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-time.Minute)) {
		token := c.accessToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	return c.refreshToken(ctx)
}

func (c *Client) refreshToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-time.Minute)) {
		return c.accessToken, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.SetBasicAuth(c.consumerKey, c.consumerSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d, message: %s", ErrUnauthorized, resp.StatusCode, string(body))
	}

	var oauthResp oauthResponse
	if err := json.Unmarshal(body, &oauthResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	c.accessToken = oauthResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(oauthResp.ExpiresIn) * time.Second)

	return c.accessToken, nil
}

type stkPushRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            int64  `json:"Amount"`
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

type stkPushResponse struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

type stkPushError struct {
	ErrorCode        string `json:"errorCode"`
	ErrorMessage     string `json:"errorMessage"`
}

func (c *Client) Charge(ctx context.Context, req payment.ChargeRequest) (payment.ChargeResult, error) {
	phone := normalizePhone(req.CustomerID)
	timestamp := formatTimestamp(time.Now())
	password := generatePassword(c.shortCode, c.passkey, timestamp)

	stkReq := stkPushRequest{
		BusinessShortCode: c.shortCode,
		Password:          password,
		Timestamp:         timestamp,
		TransactionType:   "CustomerPayBillOnline",
		Amount:            req.Amount.Value,
		PartyA:            phone,
		PartyB:            c.shortCode,
		PhoneNumber:       phone,
		CallBackURL:       c.callbackURL,
		AccountReference:  req.IDempotencyKey,
		TransactionDesc:   "Payment",
	}

	body, err := json.Marshal(stkReq)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	token, err := c.getAccessToken(ctx)
	if err != nil {
		return payment.ChargeResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/mpesa/stkpush/v1/processrequest", bytes.NewReader(body))
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

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
		c.tokenMu.Lock()
		c.accessToken = ""
		c.tokenMu.Unlock()

		token, err = c.refreshToken(ctx)
		if err != nil {
			return payment.ChargeResult{}, err
		}

		httpReq, err = http.NewRequestWithContext(ctx, "POST", c.baseURL+"/mpesa/stkpush/v1/processrequest", bytes.NewReader(body))
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("failed to read response: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		var stkErr stkPushError
		if err := json.Unmarshal(respBody, &stkErr); err == nil && stkErr.ErrorMessage != "" {
			return payment.ChargeResult{}, fmt.Errorf("%w: %s", ErrChargeFailed, stkErr.ErrorMessage)
		}
		return payment.ChargeResult{}, fmt.Errorf("%w: status %d, message: %s", ErrChargeFailed, resp.StatusCode, string(respBody))
	}

	var stkResp stkPushResponse
	if err := json.Unmarshal(respBody, &stkResp); err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if stkResp.ResponseCode != "0" {
		return payment.ChargeResult{}, fmt.Errorf("%w: %s", ErrChargeFailed, stkResp.ResponseDescription)
	}

	return payment.ChargeResult{
		ID:      stkResp.CheckoutRequestID,
		Status:  payment.StatePending,
		Message: stkResp.ResponseDescription,
	}, nil
}

type stkQueryRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	CheckoutRequestID string `json:"CheckoutRequestID"`
}

type stkQueryResponse struct {
	ResponseCode           string `json:"ResponseCode"`
	ResponseDescription    string `json:"ResponseDescription"`
	MerchantRequestID      string `json:"MerchantRequestID"`
	CheckoutRequestID      string `json:"CheckoutRequestID"`
	ResultCode             string `json:"ResultCode"`
	ResultDesc             string `json:"ResultDesc"`
}

func (c *Client) Verify(ctx context.Context, checkoutRequestID string) (payment.ChargeResult, error) {
	timestamp := formatTimestamp(time.Now())
	password := generatePassword(c.shortCode, c.passkey, timestamp)

	queryReq := stkQueryRequest{
		BusinessShortCode: c.shortCode,
		Password:          password,
		Timestamp:         timestamp,
		CheckoutRequestID: checkoutRequestID,
	}

	body, err := json.Marshal(queryReq)
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	token, err := c.getAccessToken(ctx)
	if err != nil {
		return payment.ChargeResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/mpesa/stkpushquery/v1/query", bytes.NewReader(body))
	if err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

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
		c.tokenMu.Lock()
		c.accessToken = ""
		c.tokenMu.Unlock()

		token, err = c.refreshToken(ctx)
		if err != nil {
			return payment.ChargeResult{}, err
		}

		httpReq, err = http.NewRequestWithContext(ctx, "POST", c.baseURL+"/mpesa/stkpushquery/v1/query", bytes.NewReader(body))
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return payment.ChargeResult{}, fmt.Errorf("failed to read response: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return payment.ChargeResult{}, fmt.Errorf("%w: status %d", ErrVerificationFailed, resp.StatusCode)
	}

	var queryResp stkQueryResponse
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		return payment.ChargeResult{}, fmt.Errorf("failed to parse response: %w", err)
	}

	state := mapResultCode(queryResp.ResultCode)

	return payment.ChargeResult{
		ID:      checkoutRequestID,
		Status:  state,
		Message: queryResp.ResultDesc,
	}, nil
}

func mapResultCode(code string) payment.State {
	switch code {
	case "0":
		return payment.StateCaptured
	case "1032", "1037":
		return payment.StateFailed
	default:
		return payment.StateFailed
	}
}

func (c *Client) GetPayment(id string) (*payment.Payment, error) {
	return nil, fmt.Errorf("GetPayment not supported for M-Pesa provider, use Verify instead")
}

func generatePassword(shortCode, passkey, timestamp string) string {
	data := shortCode + passkey + timestamp
	return base64.StdEncoding.EncodeToString([]byte(data))
}

func formatTimestamp(t time.Time) string {
	return t.Format("20060102150405")
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "0") {
		return "254" + phone[1:]
	}
	if strings.HasPrefix(phone, "+254") {
		return phone[1:]
	}
	return phone
}
