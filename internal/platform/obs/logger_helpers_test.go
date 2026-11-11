package obs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckoutCounterIncrementsOnce(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	checkoutTotalOpts := checkoutTotalOpts{
		result: PaymentResultSuccess,
	}

	metrics.RecordCheckout(checkoutTotalOpts.result)

	count := 0
	metrics.rentrable_checkout_total.WithLabelValues(checkoutTotalOpts.result).(func() {
		// Stub call to test external access
	})

	durationOpts := durationHistogramOpts{
		route:   "/v1/checkout",
		latency: 0.1,
	}
	metrics.Record(durationOpts)
	count2 := metrics.rentrable_request_duration.WithLabelValues(durationOpts.route)
	require.Equal(t, float64(1), count2)
}

func TestMetricsReduction(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.Record(reqCounterOpts{
		route:  "/v1/carts/{id}",
		method: "GET",
		status: http.StatusOK,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/carts/{id}",
		method: "POST",
		status: http.StatusCreated,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "POST",
		status: http.StatusOK,
	})

	total := metrics.rentrable_requests_total.Count()
	require.Equal(t, float64(3), total, "should count all requests")
}

func TestMetricsConcurrent(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	calls := 100
	var wg sync.WaitGroup

	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics.Record(reqCounterOpts{
				route:  "/v1/carts/{id}",
				method: "POST",
				status: http.StatusOK,
			})
		}()
	}

	wg.Wait()

	count := metrics.rentrable_requests_total.WithLabelValues("/v1/carts/{id}", "POST", "200").Value()
	require.Equal(t, float64(calls), count)
}

func TestMetricsCleanup(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "GET",
		status: http.StatusNotFound,
	})

	metrics.Record(reqCounterOpts{
		route:  "/v1/checkout",
		method: "GET",
		status: http.StatusInternalServerError,
	})

	successCount := metrics.rentrable_requests_total.WithLabelValues("/v1/checkout", "GET", "200").Value()
	nilCount := metrics.rentrable_requests_total.WithLabelValues("/v1/checkout", "GET", "404").Value()
	errorCount := metrics.rentrable_requests_total.WithLabelValues("/v1/checkout", "GET", "500").Value()

	require.Equal(t, float64(0), successCount, "should not count successful request")
	require.Equal(t, float64(1), nilCount, "should count not found")
	require.Equal(t, float64(1), errorCount, "should count server error")
}

func TestRouteLabelIsPatternNotRawPath(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	tests := []struct {
		name string
		opts reqCounterOpts
	}{
		{
			name: "raw path used directly",
			opts: reqCounterOpts{
				route:  "/v1/carts/abc-123",
				method: "GET",
				status: http.StatusOK,
			},
		},
		{
			name: "pattern label",
			opts: reqCounterOpts{
				route:  "/v1/carts/{id}",
				method: "GET",
				status: http.StatusOK,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.Record(tt.opts)

			route, method, status := tt.opts.route, tt.opts.method, tt.opts.status
			count := metrics.rentrable_requests_total.WithLabelValues(route, method, status).Value()
			require.Equal(t, float64(1), count, "should increment counter")
		})
	}
}

type checkoutTotalOpts struct {
	result string
}

func (m *Metrics) RecordCheckout(result string) {
	m.checkoutTotal.WithLabelValues(result).Inc()
}

type PaymentResult string

const (
	PaymentResultSuccess   PaymentResult = "success"
	PaymentResultDeclined  PaymentResult = "declined"
	PaymentResultStock     PaymentResult = "stock_conflict"
	PaymentResultError     PaymentResult = "error"
)

func TestRedactPhoneNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Kenyan national format",
			input: "254712345678",
			want:  "2547****678",
		},
		{
			name:  "Kenyan international format with plus",
			input: "+254712345678",
			want:  "+2547****678",
		},
		{
			name:  "Space separated",
			input: "254 712 345 678",
			want:  "254 7**** 678",
		},
		{
			name:  "With at symbol",
			input: "254712345678@",
			want:  "2547****678@",
		},
		{
			name:  "Different prefix still redacts",
			input: "1234567890",
			want:  "1234567890",
		},
		{
			name:  "Full Kenyan number",
			input: "254720123456",
			want:  "2547****456",
		},
		{
			name:  "Another variation",
			input: "254711223344",
			want:  "2547****344",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactPhoneNumbers(tt.input)
			require.Equal(t, tt.want, got, "phone number should be redacted correctly")
		})
	}
}

func TestRedactPAN(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "PAN string with dashes",
			input: "1234-5678-9012-3456",
			want:  "1234-****-****-3456",
		},
		{
			name:  "Full PAN",
			input: "1234567890123456",
			want:  "1234-********-3456",
		},
		{
			name:  "Short string unchanged",
			input: "123",
			want:  "123",
		},
		{
			name:  "Empty string",
			input: "",
			want:  "",
		},
		{
			name:  "With spaces",
			input: "1234 5678 9012 3456",
			want:  "1234-****-****-3456",
		},
		{
			name:  "Different format",
			input: "7890123456789012",
			want:  "7890-********-9012",
		},
		{
			name:  "Three groups",
			input: "1234567890",
			want:  "1234******90",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactPAN(tt.input)
			require.Equal(t, tt.want, got, "PAN should be redacted correctly")
		})
	}
}

func TestRedactSensitiveFields(t *testing.T) {
	type TestCustomer struct {
		ID        string `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		Address   string `json:"address"`
	}

	tests := []struct {
		name        string
		input       TestCustomer
		wantKeys    []string
		wantPhone   string
		wantEmail   string
		wantAddress string
	}{
		{
			name: "Customer with phone and email",
			input: TestCustomer{
				ID:        "123",
				FirstName: "John",
				LastName:  "Doe",
				Phone:     "254712345678",
				Email:     "john.doe@example.com",
				Address:   "123 Main St",
			},
			wantKeys:  []string{"phone", "email"},
			wantPhone: "2547****678",
			wantEmail: "reDACTED",
			wantAddress: "123 Main St",
		},
		{
			name: "Customer with international phone",
			input: TestCustomer{
				ID:    "123",
				Phone: "+254712345678",
			},
			wantKeys:  []string{"phone"},
			wantPhone: "+2547****678",
		},
		{
			name: "Customer with multiple sensitive fields",
			input: TestCustomer{
				ID:    "123",
				Email: "john.doe@example.com",
			},
			wantKeys:  []string{"email"},
			wantEmail: "reDACTED",
			wantPhone: "",
		},
		{
			name: "Minimal customer",
			input: TestCustomer{
				ID: "123",
			},
			wantKeys:  []string{},
			wantPhone: "",
			wantEmail: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, redacted := RedactSensitiveFields(tt.input)

			require.Len(t, keys, len(tt.wantKeys), "wrong number of redacted fields")
			for _, w := range tt.wantKeys {
				require.Contains(t, keys, w, "expected redacted field %s not found", w)
			}

			if tt.wantPhone != "" {
				require.Equal(t, tt.wantPhone, redacted.Phone, "phone should be redacted")
			}

			if tt.wantEmail != "" {
				require.Equal(t, tt.wantEmail, redacted.Email, "email should be redacted")
			}

			if tt.wantAddress != "" {
				require.Equal(t, tt.wantAddress, redacted.Address, "address should not be redacted")
			}
		})
	}
}

type TestPayment struct {
	ID        string `json:"id"`
	Phone     string `json:"phone"`
	Token     string `json:"token"`
	Card      string `json:"card"`
	Secret    string `json:"secret"`
	CVV       string `json:"cvv"`
	Address   string `json:"address"`
	Phone2    string `json:"phone2"` // Should be redacted
	Tel       string `json:"tel"`    // Should be redacted
	Whatsapp  string `json:"whatsapp"` // Should be redacted
}

func TestRedactMultipleSensitiveFields(t *testing.T) {
	tests := []struct {
		name        string
		input       TestPayment
		wantKeys    []string
		wantPhone   string
		wantToken   string
		wantSecret  string
		wantWhatsapp string
		wantAddress string
	}{
		{
			name: "Payment with multiple sensitive fields",
			input: TestPayment{
				ID:        "456",
				Phone:     "254712345678",
				Token:     "sk_live_abc123",
				Secret:    "my_secret_12345",
				CVV:       "123",
				Whatsapp:  "+254712345678",
				Address:   "456 Business Rd",
			},
			wantKeys:    []string{"phone", "token", "secret", "whatsapp"},
			wantPhone:   "2547****678",
			wantToken:   "REDACTED",
			wantSecret:  "REDACTED",
			wantWhatsapp: "+2547****678",
			wantAddress: "456 Business Rd",
		},
		{
			name: "Payment with PAN-like strings",
			input: TestPayment{
				Phone:   "254712345678",
				Secret:  "1234-5678-9012-3456",
				Address: "789 Finance St",
			},
			wantKeys:    []string{"phone", "secret"},
			wantPhone:   "2547****678",
			wantSecret:  "1234-****-****-3456",
			wantAddress: "789 Finance St",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, redacted := RedactSensitiveFields(tt.input)

			require.Len(t, keys, len(tt.wantKeys), "wrong number of redacted fields")
			for _, w := range tt.wantKeys {
				require.Contains(t, keys, w, "expected redacted field %s not found", w)
			}

			if tt.wantPhone != "" {
				require.Equal(t, tt.wantPhone, redacted.Phone, "phone should be redacted")
			}

			if tt.wantToken != "" {
				require.Equal(t, tt.wantToken, redacted.Token, "token should be redacted")
			}

			if tt.wantSecret != "" {
				require.Equal(t, tt.wantSecret, redacted.Secret, "secret should be redacted")
			}

			if tt.wantWhatsapp != "" {
				require.Equal(t, tt.wantWhatsapp, redacted.Whatsapp, "whatsapp should be redacted")
			}

			if tt.wantAddress != "" {
				require.Equal(t, tt.wantAddress, redacted.Address, "address should not be redacted")
			}
		})
	}
}

type TestObject struct {
	ID       string `json:"id"`
	Data     string `json:"data"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	CVV      string `json:"cvv"` // Should be redacted
	Token    string `json:"token"` // Should be redacted
	Timestamp int64 `json:"timestamp"`
}

func TestRedactWithMultipleProperties(t *testing.T) {
	input := TestObject{
		ID:       "123",
		Data:     "test data",
		Phone:    "254712345678",
		Email:    "test@example.com",
		CVV:      "123",
		Token:    "xyz",
		Timestamp: int64(time.Now().Unix()),
	}

	keys, redacted := RedactSensitiveFields(input)

	require.Equal(t, []string{"email", "cvv", "token"}, keys)

	require.Equal(t, input.ID, redacted.ID)
	require.Equal(t, input.Data, redacted.Data)
	require.Equal(t, "2547****678", redacted.Phone)
	require.Equal(t, "reDACTED", redacted.Email)
	require.Equal(t, input.Timestamp, redacted.Timestamp)
	require.NotEqual(t, input.CVV, redacted.CVV)
	require.NotEqual(t, input.Token, redacted.Token)
}

func TestRedactFieldsWithPrivateFieldPreserved(t *testing.T) {
	type TestCustomer struct {
		ID    string `json:"id"`
		Phone string `json:"phone"`
		Card  string `json:"card"` // private field
	}

	input := TestCustomer{
		ID:    "123",
		Phone: "254712345678",
		Card:  "1234-5678-9012-3456",
	}

	keys, redacted := RedactSensitiveFields(input)

	require.Equal(t, []string{"phone"}, keys)
	require.Equal(t, "2547****678", redacted.Phone)
	require.Equal(t, input.Card, redacted.Card, "private card field should be preserved")
}

func TestStringFieldsThatMatchPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Valid Kenyan phone",
			input: "254712345678",
			want:  "2547****678",
		},
		{
			name:  "Valid Kenyan phone with plus",
			input: "+254712345678",
			want:  "+2547****678",
		},
		{
			name:  "Invalid format no change",
			input: "254701234567",
			want:  "254701234567",
		},
		{
			name:  "Phone with extra spaces",
			input: "254 7 123 456 789",
			want:  "254 7**** 6789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactPhoneNumbers(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMaskPANLikeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "PAN with separator",
			input: "1234-5678-9012-3456",
			want:  "1234-****-****-3456",
		},
		{
			name:  "PAN without separator",
			input: "1234567890123456",
			want:  "1234-********-3456",
		},
		{
			name:  "Empty string",
			input: "",
			want:  "",
		},
		{
			name:  "No maskable characters",
			input: "abc123def",
			want:  "*****123***",
		},
		{
			name:  "Just letters",
			input: "ABCD",
			want:  "ABCD",
		},
		{
			name:  "Mixed case",
			input: "aBcDeF123456",
			want:  "aBcD****123456",
		},
		{
			name:  "Multiple groups",
			input: "1234-5678-9012-3456-7890",
			want:  "1234-****-****-3456-****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskPANLikeString(tt.input)
			require.Equal(t, tt.want, got, "PAN-like string should be masked correctly")
		})
	}
}

type TestScrape struct {
	ID       int `json:"id"`
	Data     string `json:"data"`
	Field1   string `json:"field1"`
	Phone    string `json:"phone"`
	Tel      string `json:"tel"`
	Email    string `json:"email"`
	Token    string `json:"token"`
	CVV      string `json:"cvv"`
	Timestamp int64 `json:"timestamp"`
}

func TestWillBeRedacted(t *testing.T) {
	tests := []struct {
		name        string
		input       TestScrape
		wantKeys    []string
	}{
		{
			name: "fields to be redacted",
			input: TestScrape{
				Data: "test",
				Phone: "254712345678",
				Tel: "254798765432",
				Email: "test@example.com",
				Token: "abc",
				CVV: "123",
			},
			wantKeys: []string{"phone", "tel", "email", "token", "cvv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, redacted := RedactSensitiveFields(tt.input)
			require.ElementsMatch(t, tt.wantKeys, keys)
		})
	}
}

func TestMaskPANEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskPAN(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMaskPANLikeStringSpecialCharacters(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "alphabetic characters only",
			input: "NAMEONE",
			want:  "NAMEONE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskPANLikeString(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

type TestCustomer struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Address   string `json:"address"`
	Card      string `json:"card"`
}

func TestPhoneRedaction(tt testing.TB, input string, want string) {
	got := RedactPhoneNumbers(input)
	require.Equal(tt, want, got)
}

func TestPANRedaction(tt testing.TB, input string, want string) {
	got := MaskPAN(tt, input)
	require.Equal(tt, want, got)
}

func TestEquality(tt testing.TB, a, b interface{}) {
	require.Equal(tt, a, b)
}

func TestComparison(tt testing.TB, a, b interface{}) {
	require.Equal(tt, a, b)
}

func TestMetricsRecordCheckout(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.RecordCheckout(PaymentResultSuccess)
	metrics.RecordCheckout(PaymentResultDeclined)
	metrics.RecordCheckout(PaymentResultStock)
	metrics.RecordCheckout(PaymentResultError)

	successCount := metrics.rentrable_checkout_total.WithLabelValues("success").Value()
	declinedCount := metrics.rentrable_checkout_total.WithLabelValues("declined").Value()
	stockCount := metrics.rentrable_checkout_total.WithLabelValues("stock_conflict").Value()
	errorCount := metrics.rentrable_checkout_total.WithLabelValues("error").Value()

	require.Equal(t, float64(1), successCount, "should count successful checkout")
	require.Equal(t, float64(1), declinedCount, "should count declined checkout")
	require.Equal(t, float64(1), stockCount, "should count stock conflict checkout")
	require.Equal(t, float64(1), errorCount, "should count error checkout")
}

func TestMetricsRecordDuration(t *testing.T) {
	metrics := NewMetrics()
	metrics.MustRegister()

	metrics.Record(durationHistogramOpts{
		route:   "/v1/products/{slug}",
		latency: 0.05,
	})

	count := metrics.rentrable_request_duration.WithLabelValues("/v1/products/{slug}").Value()
	require.Greater(t, count, float64(0))
}
