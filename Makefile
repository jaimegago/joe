.PHONY: run run-joe run-joe-core run-default run-ui run-stack build build-joe build-joe-core test clean fmt vet deps test-coverage-packages

# Run joe-core (daemon) - start this first
run-joe-core:
	go run ./cmd/joe-core

# Run joe (CLI) - requires joe-core to be running
run-joe:
	go run ./cmd/joe

# Run joe with default config location
run-default:
	go run ./cmd/joe

# Run the web UI dev server (requires joe-core to be running)
run-ui:
	cd ui && npm run dev

# Run joe-core + web UI together (requires two terminals; use this as a reminder)
# In practice, run each in a separate terminal: make run-joe-core / make run-ui
run-stack:
	@echo "Start the stack in two terminals:"
	@echo "  Terminal 1: make run-joe-core"
	@echo "  Terminal 2: make run-ui"
	@echo ""
	@echo "Then open http://localhost:5173"

# Alias: "make run" starts joe-core (the component you run first)
run: run-joe-core

# Build all binaries
build: build-joe build-joe-core

build-joe:
	go build -o joe ./cmd/joe

build-joe-core:
	go build -o joe-core ./cmd/joe-core

# Run all tests
test:
	go test ./...

# Run unit tests only (fast, no external dependencies)
test-unit:
	@set -e; \
	for pkg in $$(go list ./internal/...); do \
		echo "==> $$pkg"; \
		go test -v -short -failfast $$pkg; \
	done

# Run integration tests with mocks (no external services required)
test-integration:
	go test -v -failfast -tags=integration ./test/integration/... -timeout 2m

# Run end-to-end tests (requires building binaries)
test-e2e: build
	go test -v -tags=e2e ./test/e2e/... -timeout 5m

# Run all test types sequentially
test-all:
	go test -v -short -failfast $(shell go list ./internal/...) && \
	go test -v -failfast -tags=integration ./test/integration/... -timeout 2m && \
	go test -v -failfast -tags=e2e ./test/e2e/... -timeout 5m

# Run tests with coverage
test-coverage:
	go test -cover ./...

# Run unit tests with coverage report
test-coverage-unit:
	go test -cover -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run tests with per-package coverage percentage
test-coverage-packages:
	@set -e; \
	printf "%-60s %s\n" "package" "coverage"; \
	for pkg in $$(go list ./...); do \
		out=$$(mktemp -t coverpkg); \
		go test -coverprofile=$$out $$pkg >/dev/null; \
		pct=$$(go tool cover -func=$$out | awk '/^total:/{print $$NF}'); \
		printf "%-60s %s\n" $$pkg $$pct; \
		rm -f $$out; \
	done

# Run integration tests with coverage
test-coverage-integration:
	go test -tags=integration -cover -coverprofile=coverage-integration.out ./test/integration/...
	go tool cover -html=coverage-integration.out -o coverage-integration.html

# Run tests verbosely
test-verbose:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f joe joe-core

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
