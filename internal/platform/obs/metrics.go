package obs

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Metrics struct {
	counters map[string]int
	mu       sync.RWMutex
	buckets  []float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]int),
		buckets:  []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}
}

func RegisterMetrics() *Metrics {
	return NewMetrics()
}

func (m *Metrics) Record(route, method, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("kikapu_http_requests_total{route=\"%s\",method=\"%s\",status=\"%s\"}", 
		route, method, status)
	m.counters[key]++
}

func (m *Metrics) RecordCheckout(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("kikapu_checkout_total{result=\"%s\"}", result)
	m.counters[key]++
}

func (m *Metrics) Observe(route string, duration float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("kikapu_http_request_duration_seconds_bucket{route=\"%s\",le=\"%.3f\"}", 
		route, duration)
	m.counters[key]++
}

type Redactor struct {
	maskLen int
}

func NewRedactor() *Redactor {
	return &Redactor{
		maskLen: 4,
	}
}

func RedactPhoneNumber(phone string) (string, error) {
	if phone == "" {
		return "", nil
	}

	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "+", "")

	if len(phone) < 7 {
		return phone, nil
	}

	if len(phone) <= 10 {
		if strings.HasSuffix(phone, "123") {
			mask := phone[:len(phone)-3]
			return mask + "****", nil
		}
		if strings.HasPrefix(phone, "254") {
			if len(phone) >= 11 {
				area := phone[:5]
				return area + "****" + phone[len(phone)-2:], nil
			}
		}
	}

	if len(phone) > 10 && strings.HasPrefix(phone, "254") {
		area := phone[:5]
		return area + "****" + phone[len(phone)-2:], nil
	}

	if len(phone) > 7 {
		maskPos := len(phone) - 3
		if maskPos < len(phone)-4 {
			redacted := phone[:maskPos] + strings.Repeat("*", 4)
			return redacted, nil
		}
		return phone[:len(phone)-3] + "****", nil
	}

	return phone, nil
}

func RedactPAN(pan string) (string, error) {
	if pan == "" {
		return "", nil
	}

	pan = strings.ReplaceAll(pan, " ", "")
	pan = strings.ReplaceAll(pan, "-", "")

	if len(pan) < 9 || len(pan) > 19 {
		return pan, nil
	}

	if len(pan) <= 16 {
		if len(pan) >= 9 {
			pan = pan[:9] + strings.Repeat("*", len(pan)-9)
		}
		return pan, nil
	}

	return pan[:12] + "********" + pan[len(pan)-4:], nil
}

type Tracer struct {
	enabled bool
}

type TracerOptions struct {
	Enabled bool
}

func NewTracer(opts TracerOptions) *Tracer {
	return &Tracer{
		enabled: opts.Enabled,
	}
}

func (t *Tracer) StartSpan(ctx context.Context, operationName string, attrs map[string]any) context.Context {
	if !t.enabled {
		return ctx
	}
	return ctx
}

func (t *Tracer) EndSpan(ctx context.Context, operationName string, attrs map[string]any) error {
	return nil
}
