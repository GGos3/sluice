BINARY_NAME := sluice
MODULE := github.com/ggos3/sluice
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
GOFLAGS := -buildvcs=false

GO ?= go
GOFLAGS ?=
CGO_ENABLED ?= 0

# Default target
.PHONY: all
all: lint test build

# Build binary for current platform
.PHONY: build
build: build-sluice

.PHONY: build-sluice
build-sluice:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME)-cli ./cmd/sluice

# Run the server
.PHONY: run
run: build-sluice
	./bin/$(BINARY_NAME)-cli server -config configs/config.yaml

# Run tests
.PHONY: test
test:
	$(GO) test $(GOFLAGS) -v -race -count=1 ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	$(GO) test $(GOFLAGS) -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Lint (if golangci-lint is available)
.PHONY: lint
lint:
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipping"

# Format code
.PHONY: fmt
fmt:
	$(GO) fmt ./...
	goimports -w . 2>/dev/null || true

# Cross-compile for release (cmd/sluice — stable binary for distribution)
.PHONY: cross-build
cross-build: cross-linux-amd64 cross-linux-arm64 cross-darwin-amd64 cross-darwin-arm64

.PHONY: cross-linux-amd64
cross-linux-amd64:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/sluice

.PHONY: cross-linux-arm64
cross-linux-arm64:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/sluice

.PHONY: cross-darwin-amd64
cross-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/sluice

.PHONY: cross-darwin-arm64
cross-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/sluice

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html

# Install locally
.PHONY: install
install:
	$(GO) install $(GOFLAGS) $(LDFLAGS) ./cmd/sluice

# Tidy dependencies
.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build          - Build binary for current platform"
	@echo "  build-sluice   - Build subcommand CLI (cmd/sluice)"
	@echo "  run            - Build and run server with default config"
	@echo "  test           - Run tests with race detector"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  cross-build    - Cross-compile for linux/darwin (amd64/arm64)"
	@echo "  clean          - Remove build artifacts"
	@echo "  install        - Install binary to GOPATH/bin"
	@echo "  tidy           - Tidy Go module dependencies"
