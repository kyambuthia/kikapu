#!/bin/bash

echo "Kikapu Observability System - Benchmark Results"
echo "=================================================="
echo ""

echo "Running benchmarks..."
echo ""

echo "1. Request Tracker Performance"
echo "-------------------------------"
go test -bench=BenchmarkRequestTracker -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "2. Redaction Performance"
echo "------------------------"
go test -bench=BenchmarkRedaction -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "3. Counter Operations"
echo "--------------------"
go test -bench=BenchmarkCounter -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "4. Middleware Handler Performance"
echo "---------------------------------"
go test -bench=BenchmarkMiddlewareHandlerRequestLogging -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "5. Metrics Recorder Performance"
echo "--------------------------------"
go test -bench=BenchmarkMetricsRecorder -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "6. Checkout Counter Performance"
echo "------------------------------"
go test -bench=BenchmarkCheckoutCounter -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "7. Tracer Performance"
echo "--------------------"
go test -bench=BenchmarkTracerSpanTracking -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "8. Error Handling and Recovery"
echo "-----------------------------"
go test -bench=BenchmarkTracerErrors -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "9. Complex Combinations"
echo "----------------------"
go test -bench=BenchmarkComplexCombinations -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "10. Security Tests"
echo "-----------------"
go test -bench=BenchmarkRedactionSecurity -run=^$ ./internal/platform/obs/ 2>&1 | head -20

echo ""
echo "All benchmarks completed!"
echo ""
echo "Summary of key metrics:"
echo ""

echo "Memory allocations per operation:"
go test -bench=. -benchmem -run=^$ ./internal/platform/obs/ 2>&1 | grep -E "Benchmark|alloc=" | head -15

echo ""
echo "Testing observability functionality:"
echo "-----------------------------------"
go test -v ./internal/platform/obs/ 2>&1 | grep -E "Test|PASS|FAIL"

echo ""
echo "=== Setup Instructions ==="
echo ""
echo "1. Create a metrics-enabled server:"
echo "   go run cmd/kikapu/main.go --metrics-addr :9090"
echo ""
echo "2. Access metrics endpoint:"
echo "   curl http://localhost:9090/metrics"
echo ""
echo "3. Test basic HTTP endpoint:"
echo "   curl http://localhost:8080/healthz"
echo ""
echo "4. Run integration tests:"
echo "   make test"
echo ""
echo "5. Run with race detector:"
echo "   go test -race ./... 2>&1 | grep -E 'PASS|FAIL|NO'RACE"
echo ""
echo "=== Verification Steps ==="
echo ""
echo "1. Check format: gofmt -l internal/platform/obs/"
echo "2. Check vet: go vet ./internal/platform/obs/"
echo "3. Check staticcheck: staticcheck ./internal/platform/obs/"
echo "4. Check build: go build ./internal/platform/obs/"
echo "5. Check tests: go test -race ./internal/platform/obs/"
echo ""
