package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type ListScenario struct{}

func (s *ListScenario) Name() string {
	return "catalog list"
}

func (s *ListScenario) Run(ctx context.Context, client *Client, metrics *metrics) error {
	start := time.Now()
	defer func() {
		metrics.Lock()
		elapsed := time.Since(start).Milliseconds()
		metrics.totalReqs++
		metrics.successes++
		metrics.totalLatency += elapsed
		metrics.reqTimes = append(metrics.reqTimes, elapsed)

		if elapsed < metrics.minLatency || metrics.minLatency == 0 {
			metrics.minLatency = elapsed
		}
		if elapsed > metrics.maxLatency {
			metrics.maxLatency = elapsed
		}
		metrics.Unlock()
	}()

	cursor := ""
	var limit = 20

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var queryParams = url.Values{}
		queryParams.Set("limit", strconv.Itoa(limit))

		if cursor != "" {
			queryParams.Set("cursor", cursor)
		}

		var headers = make(map[string]string)
		resp, err := client.doGet("/v1/products?"+queryParams.Encode(), headers)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status: %d", resp.StatusCode)
		}

		var result struct {
			Data       []string `json:"data"`
			NextCursor string   `json:"next_cursor,omitempty"`
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}

		if len(result.NextCursor) == 0 {
			return nil
		}

		cursor = result.NextCursor
	}
}

type DetailScenario struct{}

func (s *DetailScenario) Name() string {
	return "product detail"
}

func (s *DetailScenario) Run(ctx context.Context, client *Client, metrics *metrics) error {
	start := time.Now()
	defer func() {
		metrics.Lock()
		elapsed := time.Since(start).Milliseconds()
		metrics.totalReqs++
		metrics.successes++
		metrics.totalLatency += elapsed
		metrics.reqTimes = append(metrics.reqTimes, elapsed)

		if elapsed < metrics.minLatency || metrics.minLatency == 0 {
			metrics.minLatency = elapsed
		}
		if elapsed > metrics.maxLatency {
			metrics.maxLatency = elapsed
		}
		metrics.Unlock()
	}()

	var headers = make(map[string]string)
	resp, err := client.doGet("/v1/products/valid-product-slug", headers)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	var result string
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}

	return nil
}

type CheckoutScenario struct{}

func (s *CheckoutScenario) Name() string {
	return "checkout"
}

func (s *CheckoutScenario) Run(ctx context.Context, client *Client, metrics *metrics) error {
	start := time.Now()
	defer func() {
		metrics.Lock()
		elapsed := time.Since(start).Milliseconds()
		metrics.totalReqs++
		metrics.successes++
		metrics.totalLatency += elapsed
		metrics.reqTimes = append(metrics.reqTimes, elapsed)

		if elapsed < metrics.minLatency || metrics.minLatency == 0 {
			metrics.minLatency = elapsed
		}
		if elapsed > metrics.maxLatency {
			metrics.maxLatency = elapsed
		}
		metrics.Unlock()
	}()

	var headers = make(map[string]string)

	var body = map[string]string{
		"cart_id": "test-cart-id",
	}

	resp, err := client.doPost("/v1/checkout", body, headers)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	var result struct {
		OrderID string `json:"order_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return err
	}

	if result.OrderID == "" {
		return fmt.Errorf("missing order_id")
	}

	return nil
}
