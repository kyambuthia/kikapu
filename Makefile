.PHONY: build test vet fmt run clean

BINARY := kikapu
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/kikapu

test:
	go test -v ./...

vet:
	go vet ./...

fmt:
	gofmt -w -l .

run:
	go run ./cmd/kikapu --addr :8080 --mode api

clean:
	rm -f $(BINARY)
