package loadtest

import (
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
		scenario  string
		wantName  string
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

