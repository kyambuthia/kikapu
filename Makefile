.PHONY: build test vet fmt run clean lint coverage

BINARY := kikapu
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/kikapu

test:
	go test -race -coverprofile=coverage.out -v ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

coverage-html:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

vet:
	go vet ./...

fmt:
	gofmt -w -l .
	goimports -w -l .

lint:
	golangci-lint run
	staticcheck ./...

run:
	go run ./cmd/kikapu --addr :8080 --mode api

clean:
	rm -f $(BINARY) coverage.out

install-golangci-lint:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.57.2

obs-test:
	go test -race -v ./internal/platform/obs/

obs-bench:
	go test -bench=Benchmark -run=^$ ./internal/platform/obs/ -benchmem

obs-coverage:
	go test -coverprofile=coverage.out ./internal/platform/obs/
	go tool cover -func=coverage.out

obs-verify:
	gofmt -l internal/platform/obs/ && echo "Format check failed"
	go vet ./internal/platform/obs/
	staticcheck ./internal/platform/obs/
