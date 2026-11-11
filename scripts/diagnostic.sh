#!/bin/bash

echo "=== Kikapu Observability System Diagnostic ==="
echo ""

echo "1. Checking file structure..."
echo "----------------------------"
find internal/platform/obs -type f -name "*.go" | sort

echo ""
echo "2. Running code quality checks..."
echo "----------------------------------"

echo "Go format check:"
gofmt -l internal/platform/obs/

echo ""
echo "Go vet check:"
go vet ./internal/platform/obs/

echo ""
echo "Static check:"
staticcheck ./internal/platform/obs/ 2>&1

echo ""
echo "Go build check:"
go build ./internal/platform/obs/

echo ""
echo "3. Running tests..."
echo "-------------------"
go test -race ./internal/platform/obs/ 2>&1 | tail -20

echo ""
echo "4. Running coverage analysis..."
echo "--------------------------------"
go test -cover ./internal/platform/obs/ 2>&1

echo ""
echo "5. Running benchmarks..."
echo "-----------------------"
### Simple benchmark run
go test -bench=BenchmarkRequestTracker -run=^$ ./internal/platform/obs/ 2>&1 | grep -E "Benchmark|ns/op"

echo ""
echo "6. Verifying metric endpoint implementation..."
echo "-----------------------------------------------"
- grep -q "func.*metricsHandler" internal/platform/obs/metrics.go && echo "✓ Metrics handler defined" || echo "✗ Metrics handler missing"
- grep -q "RegisterMetrics" internal/platform/obs/metrics.go && echo "✓ RegisterMetrics function defined" || echo "✗ RegisterMetrics missing"
- grep -q "metricsAddr" cmd/kikapu/main.go && echo "✓ Metrics address configured" || echo "✗ Metrics address not configured"
- grep -q "NewRequestCounter" internal/platform/obs/*.go && echo "✓ Request counter defined" || echo "✗ Request counter missing"
- grep -q "NewRequestDuration" internal/platform/obs/*.go && echo "✓ Request duration defined" || echo "✗ Request duration missing"
- grep -q "NewCheckoutCounter" internal/platform/obs/*.go && echo "✓ Checkout counter defined" || echo "✗ Checkout counter missing"

echo ""
echo "7. Checking middleware implementation..."
echo "----------------------------------------"
- grep -q "middleware.go" internal/platform/obs/*.go && echo "✓ Middleware file exists" || echo "✗ Middleware file missing"
- grep -q "obs.*Middleware" cmd/kikapu/main.go && echo "✓ Middleware wired in main" || echo "✗ Middleware not wired"
- grep -q "NewMiddleware" internal/platform/obs/*.go && echo "✓ NewMiddleware defined" || echo "✗ NewMiddleware missing"
- grep -q "RequestTracker" internal/platform/obs/*.go && echo "✓ Request tracker defined" || echo "✗ Request tracker missing"

echo ""
echo "8. Checking redaction functions..."
echo "----------------------------------"
- grep -q "RedactPhoneNumber" internal/platform/obs/*.go && echo "✓ Phone redaction defined" || echo "✗ Phone redaction missing"
- grep -q "RedactPAN" internal/platform/obs/*.go && echo "✓ PAN redaction defined" || echo "✗ PAN redaction missing"
- grep -q "RedactEmail" internal/platform/obs/*.go && echo "✓ Email redaction defined" || echo "✗ Email redaction missing"
- grep -q "RedactSensitiveData" internal/platform/obs/*.go && echo "✓ General redaction helper defined" || echo "✗ General redaction helper missing"
- grep -q "RedactUserAgent" internal/platform/obs/*.go && echo "✓ User agent redaction defined" || echo "✗ User agent redaction missing"

echo ""
echo "9. Checking tracer implementation..."
echo "--------------------------------------"
- grep -q "Tracer" internal/platform/obs/*.go && echo "✓ Tracer defined" || echo "✗ Tracer missing"
- grep -q "OTEL_EXPORTER_OTLP_ENDPOINT" internal/platform/obs/*.go && echo "✓ OTEL integration present" || echo "✗ OTEL integration missing"
- grep -q "tracing.go" internal/platform/obs/*.go && echo "✓ Tracer package defined" || echo "✗ Tracer package missing"

echo ""
echo "10. Checking test coverage..."
echo "----------------------------"

total_tests=$(go test ./internal/platform/obs/ 2>&1 | grep -oP '\d+PASS' | grep -oP '\d+' | sort -n | tail -1 || echo "0")
total_failures=$(go test ./internal/platform/obs/ 2>&1 | grep -oP '\d+FAIL' | grep -oP '\d+' | sort -n | tail -1 || echo "0")
total_errors=$(go test ./internal/platform/obs/ 2>&1 | grep -oP '\d+err' | grep -oP '\d+' | sort -n | tail -1 || echo "0")

echo "Total test files: ${tests_files:-unknown}"
echo "Passed tests: ${total_tests:-unknown}"
echo "Failed tests: ${total_failures:-unknown}"
echo "Errors: ${total_errors:-unknown}"

echo ""
coverage=$(go test -coverprofile=coverage.out ./internal/platform/obs/ 2>&1 | grep -oP '[0-9.]+%' | head -1 || echo "0")
echo "Code coverage: ${coverage}%"

echo ""
echo "=== Diagnostic Complete ==="
echo ""
echo "Recommended next steps:"
echo "1. Review any failed checks above"
echo "2. Update tests for redaction patterns"
echo "3. Add integration tests for endpoint behavior"
echo "4. Create sample metrics dashboard"
echo "5. Document OTEL configuration"
echo ""
