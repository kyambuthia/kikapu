package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

type RequestCounter struct {
	counters map[string]int
	mu       sync.RWMutex
}

func NewRequestCounter() *RequestCounter {
	return &RequestCounter{
		counters: make(map[string]int),
	}
}

func (rc *RequestCounter) Add(method, route, status string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	key := formatCounterKey(route, method, status)
	if key == "" {
		return
	}
	rc.counters[key]++
}

func (rc *RequestCounter) Get(route, method, status string) int {
	if route == "" || method == "" || status == "" {
		return 0
	}

	rc.mu.RLock()
	defer rc.mu.RUnlock()

	key := formatCounterKey(route, method, status)
	if key == "" {
		return 0
	}
	return rc.counters[key]
}

type RequestDuration struct {
	histogram map[string]map[float64]int
	mu        sync.RWMutex
	buckets   []float64
}

func NewRequestDuration(buckets []float64) *RequestDuration {
	return &RequestDuration{
		histogram: make(map[string]map[float64]int),
		buckets:   buckets,
	}
}

func (rd *RequestDuration) Observe(duration float64, route string) {
	if route == "" || duration < 0 {
		return
	}

	rd.mu.Lock()
	defer rd.mu.Unlock()

	if rd.histogram[route] == nil {
		rd.histogram[route] = make(map[float64]int)
	}

	for _, bucket := range rd.buckets {
		if duration <= bucket {
			rd.histogram[route][bucket]++
			return
		}
	}

	rd.histogram[route][-1]++
}

type CheckoutCounter struct {
	counters map[string]int
	mu       sync.RWMutex
}

func NewCheckoutCounter() *CheckoutCounter {
	return &CheckoutCounter{
		counters: make(map[string]int),
	}
}

func (cc *CheckoutCounter) Add(method, route, status string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	key := formatCounterKey(route, method, status)
	if key == "" {
		return
	}
	cc.counters[key]++
}

func (cc *CheckoutCounter) GetCheckoutCount(result string) int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	key := fmt.Sprintf("checkout_total{result=\"%s\"}", result)
	return cc.counters[key]
}

func (cc *CheckoutCounter) IncrementCheckout(result string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	key := fmt.Sprintf("checkout_total{result=\"%s\"}", result)
	cc.counters[key]++
}

func formatCounterKey(route, method, status string) string {
	if route == "" || method == "" || status == "" {
		return ""
	}

	parts := []string{
		fmt.Sprintf("method=\"%s\"", method),
		fmt.Sprintf("route=\"%s\"", route),
		fmt.Sprintf("status=\"%s\"", status),
	}

	return "kikapu_http_requests_total{" + joinQuoted(parts) + "}"
}

func joinQuoted(parts []string) string {
	length := len(parts)
	if length == 0 {
		return ""
	}
	if length == 1 {
		return parts[0]
	}

	result := make([]byte, 0, length*20)
	for i, part := range parts {
		if i > 0 {
			result = append(result, ' ', ',')
		}
		result = append(result, part...)
	}
	return string(result)
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if ctxValue, ok := ctx.Value(ctxKeyRequestID{}).(string); ok {
		return ctxValue
	}
	return ""
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func normalizeRoute(rawPath string) string {
	if rawPath == "" || len(rawPath) < 4 {
		return rawPath
	}

	if rawPath[4] != '/' {
		return rawPath
	}

	if len(rawPath) <= 5 {
		return rawPath
	}

	i := 5
	for i >= len(rawPath) || rawPath[i] != '/' {
		return rawPath[:4] + "/" + rawPath[i+1:]
	}

	if i >= len(rawPath) {
		return rawPath
	}

	return rawPath[:4] + "/" + rawPath[i+1:]
}

type ctxKeyRequestID struct{}

type urlKey struct{}
