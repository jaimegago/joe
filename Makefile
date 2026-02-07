.PHONY: run run-joe run-joecored run-default build build-joe build-joecored test clean fmt vet deps

# Run joecored (daemon) - start this first
run-joecored:
	go run ./cmd/joecored

# Run joe (CLI) - requires joecored to be running
run-joe:
	go run ./cmd/joe

# Run joe with default config location
run-default:
	go run ./cmd/joe

# Alias: "make run" starts joecored (the component you run first)
run: run-joecored

# Build both binaries
build: build-joe build-joecored

build-joe:
	go build -o joe ./cmd/joe

build-joecored:
	go build -o joecored ./cmd/joecored

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
	rm -f joe joecored

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
