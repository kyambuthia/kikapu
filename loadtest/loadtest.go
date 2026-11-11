package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	defaultConcurrency = 10
	defaultDuration    = 10 * time.Second
)

type Config struct {
	scenario   string
	concurrency int
	duration   time.Duration
	rps        int
	listenAddr string
}

type metrics struct {
	sync.Mutex
	successes    int64
	failures     int64
	totalLatency int64
	totalReqs    int64
	minLatency   int64
	maxLatency   int64
	reqTimes     []int64
}

type Scenario interface {
	Name() string
	Run(ctx context.Context, client *Client, metrics *metrics) error
}

type loadtest struct {
	config        Config
	client        *Client
	logger        *slog.Logger
	metrics       *metrics
	scenarioMap   map[string]Scenario
}

func NewLoadTest(config Config) (*loadtest, error) {
	client, err := NewClient(config.listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	scenarioMap := make(map[string]Scenario)
	scenarioMap["list"] = &ListScenario{}
	scenarioMap["detail"] = &DetailScenario{}
	scenarioMap["checkout"] = &CheckoutScenario{}

	return &loadtest{
		config:       config,
		client:       client,
		logger:       logger,
		metrics:      &metrics{},
		scenarioMap:  scenarioMap,
	}, nil
}

func main() {
	config := Config{}

	flag.StringVar(&config.listenAddr, "addr", "http://localhost:8080", "Address of the API server")
	flag.StringVar(&config.scenario, "scenario", "list", "Scenario to run (list, detail, checkout)")
	flag.IntVar(&config.concurrency, "c", defaultConcurrency, "Number of concurrent workers")
	flag.DurationVar(&config.duration, "d", defaultDuration, "Duration of the load test")
	flag.IntVar(&config.rps, "rps", 0, "Requests per second cap (0 = unlimited)")

	flag.Parse()

	lt, err := NewLoadTest(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := lt.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Test failed: %v\n", err)
		os.Exit(1)
	}
}

func (lt *loadtest) Run() error {
	scenario, ok := lt.scenarioMap[lt.config.scenario]
	if !ok {
		return fmt.Errorf("unknown scenario: %s", lt.config.scenario)
	}

	lt.logger.Info("Starting load test",
		slog.String("scenario", lt.config.scenario),
		slog.Int("concurrency", lt.config.concurrency),
		slog.Duration("duration", lt.config.duration),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(lt.config.concurrency)

	stopCh := time.After(lt.config.duration)

	for i := 0; i < lt.config.concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			runWorker(ctx, stopCh, scenario, lt.client, lt.metrics, lt.logger)
		}(i)
	}

	wg.Wait()

	lt.printResults()
	return nil
}

func runWorker(
	ctx context.Context,
	stopCh <-chan time.Time,
	scenario Scenario,
	client *Client,
	metrics *metrics,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			if err := scenario.Run(ctx, client, metrics); err != nil {
				metrics.Lock()
				metrics.failures++
				metrics.Unlock()
				logger.Warn("Scenario failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (lt *loadtest) printResults() {
	lt.metrics.Lock()
	defer lt.metrics.Unlock()

	totalReqs := lt.metrics.totalReqs
	if totalReqs == 0 {
		fmt.Println("No requests made")
		return
	}

	successRate := float64(lt.metrics.successes) / float64(totalReqs) * 100
	if lt.metrics.reqTimes != nil && len(lt.metrics.reqTimes) > 0 {
		stdDev := lt.calculateStdDev(lt.metrics.reqTimes)
		p50 := lt.percentile(lt.metrics.reqTimes, 0.5)
		p95 := lt.percentile(lt.metrics.reqTimes, 0.95)
		p99 := lt.percentile(lt.metrics.reqTimes, 0.99)

		fmt.Println("\n=== Load Test Results ===")
		fmt.Printf("Requests:     %d\n", totalReqs)
		fmt.Printf("Success Rate: %.2f%%\n", successRate)
		fmt.Printf("Failures:     %d\n", lt.metrics.failures)
		fmt.Printf("Throughput:   %.2f\n", float64(totalReqs)/lt.config.duration.Seconds())
		fmt.Println("\n=== Latency (ms) ===")
		fmt.Printf("P50:          %.2f ms\n", float64(p50)/1000)
		fmt.Printf("P95:          %.2f ms\n", float64(p95)/1000)
		fmt.Printf("P99:          %.2f ms\n", float64(p99)/1000)
		fmt.Printf("Min:          %.2f ms\n", float64(lt.metrics.minLatency)/1000)
		fmt.Printf("Max:          %.2f ms\n", float64(lt.metrics.maxLatency)/1000)
		fmt.Printf("Mean:         %.2f ms\n", float64(lt.metrics.totalLatency)/1000/float64(totalReqs))
		fmt.Printf("Std Dev:      %.2f ms\n", float64(stdDev)/1000)

		jsonData, _ := json.Marshal(map[string]interface{}{
			"success_rate": successRate,
			"throughput": float64(totalReqs) / lt.config.duration.Seconds(),
			"latency_p50": p50,
			"latency_p95": p95,
			"latency_p99": p99,
		})
		fmt.Printf("\n=== JSON Output ===\n%s\n", string(jsonData))
	} else {
		fmt.Println("\nNo latency data collected")
	}
}

func (lt *loadtest) calculateStdDev(times []int64) int64 {
	if len(times) == 0 {
		return 0
	}

	mean := int64(0)
	for _, t := range times {
		mean += t
	}
	mean /= int64(len(times))

	variance := int64(0)
	for _, t := range times {
		diff := t - mean
		variance += diff * diff
	}
	variance /= int64(len(times))

	return int64(math.Sqrt(float64(variance)))
}

func (lt *loadtest) percentile(times []int64, pct float64) int64 {
	if len(times) == 0 {
		return 0
	}

	n := int64(len(times))
	index := int64(float64(n-1) * pct)
	if index < 0 {
		index = 0
	}
	if index >= n {
		index = n - 1
	}

	swapped := make([]int64, len(times))
	copy(swapped, times)
	for i := 0; i < len(times)-1; i++ {
		for j := i + 1; j < len(times); j++ {
			if swapped[i] > swapped[j] {
				swapped[i], swapped[j] = swapped[j], swapped[i]
			}
		}
	}

	return swapped[index]
}
