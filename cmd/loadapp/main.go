package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/loadtest"
)

var (
	scenario     string
	concurrency  int
	duration     time.Duration
	rpsCap       int
	prettyJSON   bool
	scenarioFlag bool
	durationFlag bool
	rpsFlag      bool
	jsonFlag     bool
	topicsFlag   bool
)

func init() {
	flag.StringVar(&scenario, "scenario", "", "scenario: list, detail, checkout")
	flag.IntVar(&concurrency, "c", 20, "concurrent goroutines")
	flag.DurationVar(&duration, "d", 10*time.Second, "duration")
	flag.IntVar(&rpsCap, "rps", 0, "requests per second cap")
	flag.BoolVar(&jsonFlag, "json", false, "output in JSON format")
}

func main() {
	parseFlags()
	run()
}

func parseFlags() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected argument: %s\n", flag.Arg(0))
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Load test harness for kikapu\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  %s [flags]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s -scenario list -c 20 -d 10s\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -scenario checkout -c 50 -d 30s -rps 100\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -scenario detail -c 100 -d 60s --json\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Scenarios:\n")
	fmt.Fprintf(os.Stderr, "  list       - paginated catalog list\n")
	fmt.Fprintf(os.Stderr, "  detail     - single product detail\n")
	fmt.Fprintf(os.Stderr, "  checkout   - fake checkout process\n")
	fmt.Fprintf(os.Stderr, "\n")
}

func run() {
	cfg := loadtest.Config{
		Addr:       getEnv("KIKAPU_ADDR", ":8080"),
		Scenario:   parseScenario(scenario),
		Concurrency: concurrency,
		Duration:    duration,
		RPSCap:      rpsCap,
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	os.Exit(loadtest.Run(cfg))
}

func parseScenario(s string) string {
	if s == "" {
		os.Exit(1)
	}
	return s
}

func getEnv(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}
