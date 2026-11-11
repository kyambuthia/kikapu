.PHONY: build test vet fmt run clean lint coverage

BINARY := kikapu
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/kikapu

test:
	go test -race -v ./...

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
