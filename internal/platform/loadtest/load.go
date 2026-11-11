package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	Addr        string
	Scenario    string
	Concurrency int
	Duration    time.Duration
	RPSCap      int
	startedAt   time.Time
	endedAt     time.Time
}

func (c *Config) Validate() error {
	if c.Scenario == "" {
		return fmt.Errorf("scenario is required")
	}

	if c.Scenario != "list" && c.Scenario != "detail" && c.Scenario != "checkout" {
		return fmt.Errorf("invalid scenario: %s (valid: list, detail, checkout)", c.Scenario)
	}

	if c.Concurrency < 1 {
		return fmt.Errorf("concurrency must be at least 1")
	}

	if c.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}

	if c.RPSCap < 0 {
		return fmt.Errorf("rps cap must be non-negative")
	}

	return nil
}

var (
	config   Config
	JSONFlag bool
)

func parseFlags() (*Config, error) {
	cfg := &Config{
		Addr:        getEnv("KIKAPU_ADDR", ":8080"),
		Concurrency: parseInt(getEnv("KIKAPU_CONCURRENCY", "20"), 20),
		Duration:    parseDuration(getEnv("KIKAPU_DURATION", "10s")),
		RPSCap:      parseInt(getEnv("KIKAPU_RPS_CAP", "0"), 0),
	}

	flag.StringVar(&cfg.Scenario, "scenario", "", "scenario: list, detail, checkout")
	flag.IntVar(&cfg.Concurrency, "c", cfg.Concurrency, "concurrent goroutines")
	flag.DurationVar(&cfg.Duration, "d", cfg.Duration, "duration")
	flag.IntVar(&cfg.RPSCap, "rps", cfg.RPSCap, "requests per second cap")
	flag.BoolVar(&JSONFlag, "json", false, "output in JSON format")
	flag.Parse()

	return cfg, nil
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 10 * time.Second
	}
	return d
}

func getEnv(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}

func run(ctx context.Context, scenario string, workers int, duration time.Duration, rpsCap int) []ScenarioResult {
	var wg sync.WaitGroup
	var results []ScenarioResult
	var mu sync.Mutex

	if workers <= 0 {
		return nil
	}

	if rpsCap <= 0 {
		rpsCap = int(duration.Seconds() + 1)
	}

	ticker := time.NewTicker(time.Second / 10) // More responsive ticker
	defer ticker.Stop()

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()

			client := &http.Client{
				Timeout: duration * 10,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			scenarioResult := createScenarioResult(scenario)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			requestCount := 0
			for {
				select {
				case <-ctx.Done():
					if !scenarioResult.endedAt.IsZero() {
						return
					}
					scenarioResult.endedAt = time.Now()
					return
				case <-ticker.C:
					if requestCount < rpsCap {
						latency := processRequest(client, scenario)
						if latency > 0 {
							scenarioResult.requestLatencies = append(scenarioResult.requestLatencies, latency)
						}
						requestCount++
					}
				}
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	results = []ScenarioResult{createScenarioResult(scenario)}
	mu.Unlock()

	return results
}

type ScenarioResult struct {
	Name         string
	BaseURL      string
	Duration     string
	ErrorCount   int
	SuccessCount int
	Throughput   int
	Concurrency  int

	Latencies struct {
		P50 string
		P95 string
		P99 string
	} `json:"latencies"`

	requestLatencies []time.Duration
	startedAt        time.Time
	endedAt          time.Time
}

func createScenarioResult(scenario string) ScenarioResult {
	var result ScenarioResult
	switch scenario {
	case "list":
		result.Name = "catalog list"
	case "detail":
		result.Name = "product detail"
	case "checkout":
		result.Name = "checkout"
	default:
		result.Name = scenario
	}

	result.BaseURL = "http://" + config.Addr
	result.Duration = config.Duration.String()
	result.Throughput = 0
	result.ErrorCount = 0
	result.SuccessCount = 0
	result.Concurrency = config.Concurrency
	result.Latencies.P50 = "0ms"
	result.Latencies.P95 = "0ms"
	result.Latencies.P99 = "0ms"
	result.requestLatencies = nil
	result.startedAt = config.startedAt
	result.endedAt = config.endedAt
	return result
}

func processRequest(client *http.Client, scenario string) time.Duration {
	var latency time.Duration

	switch scenario {
	case "list":
		latency = requestCatalogList(client, "http://"+config.Addr+"/api/products")
	case "detail":
		latency = requestProductDetail(client, "http://"+config.Addr+"/api/products/1")
	case "checkout":
		latency = requestCheckout(client, "http://"+config.Addr+"/api/checkout")
	default:
		return 0
	}

	return latency
}

func requestCatalogList(client *http.Client, baseURL string) time.Duration {
	req, err := http.NewRequest("GET", baseURL+"?limit=10", nil)
	if err != nil {
		return 0
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	elapsed := time.Since(start)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return 0
	}

	resp.Body.Close()
	return elapsed
}

func requestProductDetail(client *http.Client, baseURL string) time.Duration {
	req, err := http.NewRequest("GET", baseURL+"/v1/products/product-a", nil)
	if err != nil {
		return 0
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	elapsed := time.Since(start)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return 0
	}

	resp.Body.Close()
	return elapsed
}

func requestCheckout(client *http.Client, baseURL string) time.Duration {
	body := map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_id": "product-a", "quantity": 1},
		},
		"payment_method": "test",
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return 0
	}

	req, err := http.NewRequest("POST", baseURL+"/v1/checkout", bytes.NewReader(jsonData))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	elapsed := time.Since(start)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return 0
	}

	resp.Body.Close()
	return elapsed
}

func calculatePercentiles(data []time.Duration, p float64) time.Duration {
	if len(data) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	idx := int(float64(len(sorted))*p) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000)
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.0fms", float64(d.Milliseconds()))
}

func printTable(results []ScenarioResult) {
	fmt.Println("\nPerformance Report")
	fmt.Println("==================")
	fmt.Println()

	for _, result := range results {
		fmt.Printf("Scenario: %s\n", result.Name)
		fmt.Printf("  Duration:       %s\n", result.Duration)
		fmt.Printf("  Concurrency:    %d\n", result.Concurrency)
		fmt.Printf("  Throughput:     %d requests\n", result.Throughput)
		successRate := computeSuccessRate(result.ErrorCount, result.Throughput)
		fmt.Printf("  Error Rate:     %.2f%%\n", successRate)
		fmt.Printf("  Latencies:\n")
		fmt.Printf("    p50:         %s\n", result.Latencies.P50)
		fmt.Printf("    p95:         %s\n", result.Latencies.P95)
		fmt.Printf("    p99:         %s\n", result.Latencies.P99)
		fmt.Printf("\n")
	}
}

func saveJSON(results []ScenarioResult) error {
	data := Result{
		Timestamp: time.Now().Format(time.RFC3339),
		Hostname:  "localhost",
	}
	goVersion := os.Getenv("GOVERSION")
	if goVersion == "" {
		data.GoVersion = "unknown"
	} else {
		data.GoVersion = goVersion
	}

	machineSpec := map[string]string{"cpus": "4", "memory": "16GB", "os": "Linux"}
	data.MachineSpec = machineSpec

	scenariosMap := map[string]interface{}{}
	for _, result := range results {
		scenariosMap[result.Name] = map[string]interface{}{
			"name":       result.Name,
			"base_url":   result.BaseURL,
			"duration":   result.Duration,
			"throughput": result.Throughput,
			"error_rate": computeSuccessRate(result.ErrorCount, result.Throughput),
			"latencies": map[string]interface{}{
				"p50": result.Latencies.P50,
				"p95": result.Latencies.P95,
				"p99": result.Latencies.P99,
			},
		}
	}
	data.Scenarios = scenariosMap

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(data)
}

type Result struct {
	Timestamp   string                 `json:"timestamp"`
	Hostname    string                 `json:"hostname"`
	MachineSpec map[string]string      `json:"machine_spec"`
	Scenarios   map[string]interface{} `json:"scenarios"`
	GoVersion   string                 `json:"go_version"`
}

func Run(cfg Config) int {
	config = cfg
	config.startedAt = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), config.Duration+time.Second)
	defer cancel()

	done := make(chan int, 1)

	go func() {
		results := run(ctx, config.Scenario, config.Concurrency, config.Duration, config.RPSCap)

		for _, result := range results {
			result.Throughput = len(result.requestLatencies)
			result.SuccessCount = result.Throughput
			result.ErrorCount = 0
			durationMs := config.Duration.Seconds() * 1000
			result.Duration = fmt.Sprintf("%.3fs", durationMs/1000)

			if len(result.requestLatencies) > 0 {
				result.Latencies.P50 = formatDuration(calculatePercentiles(result.requestLatencies, 0.50))
				result.Latencies.P95 = formatDuration(calculatePercentiles(result.requestLatencies, 0.95))
				result.Latencies.P99 = formatDuration(calculatePercentiles(result.requestLatencies, 0.99))
			} else {
				result.Latencies.P50 = "0ms"
				result.Latencies.P95 = "0ms"
				result.Latencies.P99 = "0ms"
			}
		}

		printTable(results)

		if JSONFlag {
			if err := saveJSON(results); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving JSON: %v\n", err)
			}
		}

		done <- 0
	}()

	select {
	case <-done:
		return 0
	case <-ctx.Done():
		return 1
	}
}

func computeSuccessRate(errors int, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(total-errors) * 100.0 / float64(total)
}
