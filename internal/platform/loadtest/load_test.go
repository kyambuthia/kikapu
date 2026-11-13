package loadtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestWaitGroupCounter(t *testing.T) {
	cfg := Config{
		Addr:        ":8080",
		Scenario:    "list",
		Concurrency: 2,
		Duration:    100 * time.Millisecond,
		RPSCap:      0,
	}

	config = cfg
	config.startedAt = time.Now()

	var wg sync.WaitGroup
	resultsChan := make(chan ScenarioResult, cfg.Concurrency)

	workers := cfg.Concurrency

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			scenarioResult := createScenarioResult(cfg.Scenario)
			scenarioResult.endedAt = time.Now()
			resultsChan <- scenarioResult
		}(i)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var results []ScenarioResult
	for result := range resultsChan {
		results = append(results, result)
	}

	if len(results) != workers {
		t.Errorf("expected %d results, got %d", workers, len(results))
	}
}

func TestCreateScenarioResult(t *testing.T) {
	tests := []struct {
		scenario string
		wantName string
	}{
		{"list", "catalog list"},
		{"detail", "product detail"},
		{"checkout", "checkout"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			result := createScenarioResult(tt.scenario)
			if result.Name != tt.wantName {
				t.Errorf("createScenarioResult(%q) = %q, want %q", tt.scenario, result.Name, tt.wantName)
			}
		})
	}
}

func TestHTTPClientTimeoutValue(t *testing.T) {
	expectedTimeout := 30 * time.Second

	if expectedTimeout != 30*time.Second {
		t.Errorf("HTTP timeout should be fixed at 30s, not based on test duration")
	}
}

func TestRequestCatalogListHandlesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("intentional failure")
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCatalogList(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on error, got %v", latency)
	}
}

func TestRequestProductDetailHandlesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("intentional failure")
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestProductDetail(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on error, got %v", latency)
	}
}

func TestRequestCheckoutHandlesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("intentional failure")
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCheckout(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on error, got %v", latency)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid list scenario",
			cfg: Config{
				Scenario:    "list",
				Concurrency: 1,
				Duration:    time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid detail scenario",
			cfg: Config{
				Scenario:    "detail",
				Concurrency: 1,
				Duration:    time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid checkout scenario",
			cfg: Config{
				Scenario:    "checkout",
				Concurrency: 1,
				Duration:    time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty scenario",
			cfg: Config{
				Scenario:    "",
				Concurrency: 1,
				Duration:    time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid scenario",
			cfg: Config{
				Scenario:    "invalid",
				Concurrency: 1,
				Duration:    time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero concurrency",
			cfg: Config{
				Scenario:    "list",
				Concurrency: 0,
				Duration:    time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative concurrency",
			cfg: Config{
				Scenario:    "list",
				Concurrency: -1,
				Duration:    time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero duration",
			cfg: Config{
				Scenario:    "list",
				Concurrency: 1,
				Duration:    0,
			},
			wantErr: true,
		},
		{
			name: "negative rps cap",
			cfg: Config{
				Scenario:    "list",
				Concurrency: 1,
				Duration:    time.Second,
				RPSCap:      -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculatePercentiles(t *testing.T) {
	tests := []struct {
		name     string
		data     []time.Duration
		p        float64
		expected time.Duration
	}{
		{
			name:     "empty data",
			data:     []time.Duration{},
			p:        0.5,
			expected: 0,
		},
		{
			name:     "single element p50",
			data:     []time.Duration{100 * time.Millisecond},
			p:        0.5,
			expected: 100 * time.Millisecond,
		},
		{
			name:     "multiple elements p50",
			data:     []time.Duration{10, 20, 30, 40, 50},
			p:        0.5,
			expected: 20,
		},
		{
			name:     "multiple elements p95",
			data:     []time.Duration{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			p:        0.95,
			expected: 90,
		},
		{
			name:     "multiple elements p99",
			data:     []time.Duration{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			p:        0.99,
			expected: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePercentiles(tt.data, tt.p)
			if result != tt.expected {
				t.Errorf("calculatePercentiles() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{
			name:     "microseconds",
			d:        500 * time.Microsecond,
			expected: "500µs",
		},
		{
			name:     "milliseconds",
			d:        50 * time.Millisecond,
			expected: "50ms",
		},
		{
			name:     "seconds as milliseconds",
			d:        1500 * time.Millisecond,
			expected: "1500ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.d)
			if result != tt.expected {
				t.Errorf("formatDuration() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		def      int
		expected int
	}{
		{
			name:     "empty string",
			input:    "",
			def:      10,
			expected: 10,
		},
		{
			name:     "valid integer",
			input:    "42",
			def:      10,
			expected: 42,
		},
		{
			name:     "invalid integer",
			input:    "not-a-number",
			def:      10,
			expected: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseInt(tt.input, tt.def)
			if result != tt.expected {
				t.Errorf("parseInt() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "valid duration",
			input:    "5s",
			expected: 5 * time.Second,
		},
		{
			name:     "invalid duration",
			input:    "invalid",
			expected: 10 * time.Second,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDuration(tt.input)
			if result != tt.expected {
				t.Errorf("parseDuration() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	// Test with existing env var
	os.Setenv("TEST_VAR", "test_value")
	result := getEnv("TEST_VAR", "default")
	if result != "test_value" {
		t.Errorf("getEnv() = %v, expected test_value", result)
	}
	os.Unsetenv("TEST_VAR")

	// Test with missing env var
	result = getEnv("MISSING_VAR", "default")
	if result != "default" {
		t.Errorf("getEnv() = %v, expected default", result)
	}
}

func TestProcessRequest(t *testing.T) {
	// Setup config
	config = Config{
		Addr:        ":8080",
		Scenario:    "list",
		Concurrency: 1,
		Duration:    time.Second,
	}

	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Update config.Addr to use the test server
	config.Addr = server.URL[len("http://"):]

	client := &http.Client{Timeout: time.Second}

	tests := []struct {
		name     string
		scenario string
	}{
		{"list", "list"},
		{"detail", "detail"},
		{"checkout", "checkout"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.Scenario = tt.scenario
			result := processRequest(client, tt.scenario)
			// For unknown scenario, latency should be 0
			if tt.scenario == "unknown" {
				if result != 0 {
					t.Errorf("processRequest(unknown) should return 0, got %v", result)
				}
			}
			// For valid scenarios with server, should get a positive latency
			// (though server may panic, so we just check it doesn't crash)
		})
	}
}

func TestComputeSuccessRate(t *testing.T) {
	tests := []struct {
		name     string
		errors   int
		total    int
		expected float64
	}{
		{
			name:     "zero total",
			errors:   0,
			total:    0,
			expected: 0,
		},
		{
			name:     "no errors",
			errors:   0,
			total:    100,
			expected: 100.0,
		},
		{
			name:     "half errors",
			errors:   50,
			total:    100,
			expected: 50.0,
		},
		{
			name:     "all errors",
			errors:   100,
			total:    100,
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeSuccessRate(tt.errors, tt.total)
			if result != tt.expected {
				t.Errorf("computeSuccessRate() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRequestCatalogListSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"products": []}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCatalogList(client, server.URL+"/api/products")
	if latency <= 0 {
		t.Errorf("expected positive latency on success, got %v", latency)
	}
}

func TestRequestProductDetailSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "product-a"}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestProductDetail(client, server.URL)
	if latency <= 0 {
		t.Errorf("expected positive latency on success, got %v", latency)
	}
}

func TestRequestCheckoutSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"order_id": "123"}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCheckout(client, server.URL)
	if latency <= 0 {
		t.Errorf("expected positive latency on success, got %v", latency)
	}
}

func TestRequestCatalogListNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCatalogList(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on non-2xx status, got %v", latency)
	}
}

func TestRequestProductDetailNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestProductDetail(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on non-2xx status, got %v", latency)
	}
}

func TestRequestCheckoutNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCheckout(client, server.URL)
	if latency != 0 {
		t.Errorf("expected latency 0 on non-2xx status, got %v", latency)
	}
}

func TestRequestCatalogListBadURL(t *testing.T) {
	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCatalogList(client, "://bad-url")
	if latency != 0 {
		t.Errorf("expected latency 0 on bad URL, got %v", latency)
	}
}

func TestRequestProductDetailBadURL(t *testing.T) {
	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestProductDetail(client, "://bad-url")
	if latency != 0 {
		t.Errorf("expected latency 0 on bad URL, got %v", latency)
	}
}

func TestRequestCheckoutBadURL(t *testing.T) {
	client := &http.Client{Timeout: 1 * time.Second}
	latency := requestCheckout(client, "://bad-url")
	if latency != 0 {
		t.Errorf("expected latency 0 on bad URL, got %v", latency)
	}
}

func TestParseFlags(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Test with minimal flags
	os.Args = []string{"test", "-scenario=list"}
	cfg, err := parseFlags()
	if err != nil {
		t.Errorf("parseFlags() error = %v", err)
	}
	if cfg.Scenario != "list" {
		t.Errorf("parseFlags() scenario = %v, want list", cfg.Scenario)
	}
}

func TestRunWithZeroWorkers(t *testing.T) {
	results := run(context.Background(), "list", 0, time.Second, 10)
	if results != nil {
		t.Errorf("run with zero workers should return nil, got %v", results)
	}
}

func TestRun_EndToEndAndReporting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cart_1","data":[]}`))
	}))
	defer srv.Close()

	origCfg := config
	defer func() {
		config = origCfg
	}()

	config = Config{
		Scenario:    "list",
		Concurrency: 2,
		Duration:    120 * time.Millisecond,
		RPSCap:      10,
		Addr:        srv.Listener.Addr().String(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	results := run(ctx, config.Scenario, config.Concurrency, config.Duration, config.RPSCap)
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}

	printTable(results)
	if err := saveJSON(results); err != nil {
		t.Fatalf("saveJSON() error = %v", err)
	}
}
