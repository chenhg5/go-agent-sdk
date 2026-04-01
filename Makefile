.PHONY: build test test-v test-integration lint fmt vet clean examples

# Build all packages
build:
	go build ./...

# Run unit tests
test:
	go test ./...

# Run unit tests with verbose output
test-v:
	go test -v -count=1 ./...

# Run integration tests (requires ANTHROPIC_AUTH_TOKEN)
test-integration:
	go test -v -tags=integration -timeout 180s ./...

# Run all tests (unit + integration)
test-all: test test-integration

# Format code
fmt:
	gofmt -w .

# Static analysis
vet:
	go vet ./...

# Lint (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; exit 1; }
	golangci-lint run ./...

# Run examples
example-basic:
	go run ./examples/basic

example-tools:
	go run ./examples/tools

example-streaming:
	go run ./examples/streaming

examples: example-basic example-streaming

# Clean build cache
clean:
	go clean -cache -testcache
