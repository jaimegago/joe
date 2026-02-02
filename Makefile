.PHONY: run build test clean

# Run joe without building (uses config.yaml in current dir)
run:
	go run ./cmd/joe -config $(PWD)/config.yaml

# Run with default config location
run-default:
	go run ./cmd/joe

# Build the binary
build:
	go build -o joe ./cmd/joe

# Run all tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -cover ./...

# Run tests verbosely
test-verbose:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f joe

# Format code
fmt:
	gofmt -s -w .

# Run linter
vet:
	go vet ./...

# Install dependencies
deps:
	go mod download
	go mod tidy
