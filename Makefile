.PHONY: build test test-verbose test-coverage clean fmt vet lint help run

# Binary name
BINARY_NAME=flux-webhook-autoreconciler
# Build directory
BUILD_DIR=bin
# Main package path
MAIN_PACKAGE=./cmd

# Default target
.DEFAULT_GOAL := help

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## test: Run tests
test:
	@echo "Running tests..."
	@go test ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	@go test -v ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## test-reconciler: Run reconciler tests specifically
test-reconciler:
	@echo "Running reconciler tests..."
	@go test -v ./cmd -run TestReconcile

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

## fmt: Format Go code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Format complete"

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet complete"

## lint: Run golangci-lint (if installed)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install it with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## mod-tidy: Tidy go.mod and go.sum
mod-tidy:
	@echo "Tidying go.mod..."
	@go mod tidy
	@echo "Tidy complete"

## mod-download: Download dependencies
mod-download:
	@echo "Downloading dependencies..."
	@go mod download
	@echo "Download complete"

## run: Run the application (requires config file)
run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BUILD_DIR)/$(BINARY_NAME) -config config/server.yaml

## run-client: Run the application in client mode
run-client: build
	@echo "Running $(BINARY_NAME) in client mode..."
	@./$(BUILD_DIR)/$(BINARY_NAME) -config config/client.yaml

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
