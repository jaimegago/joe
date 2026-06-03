.PHONY: run run-joe run-ui run-stack build build-joe build-ui test clean fmt vet deps test-coverage-packages

# Directory the production UI build is staged into for go:embed. go:embed
# cannot reach ui/dist directly (it lives outside the webui package subtree),
# so build-ui copies the Vite output here. Only .gitkeep is committed; the
# staged build output is gitignored.
EMBED_UI_DIR := internal/webui/dist

# Run joe (the server daemon) - start this first
run-joe:
	go run ./cmd/joe

# Run the web UI dev server (requires joe to be running)
run-ui:
	cd ui && npm run dev

# Run joe + web UI together (requires two terminals; use this as a reminder)
# In practice, run each in a separate terminal: make run-joe / make run-ui
run-stack:
	@echo "Start the stack in two terminals:"
	@echo "  Terminal 1: make run-joe"
	@echo "  Terminal 2: make run-ui"
	@echo ""
	@echo "Then open http://localhost:5173"

# Alias: "make run" starts the joe server (the component you run first)
run: run-joe

# Build the joe binary (server + subcommands), with the production web UI
# embedded via the build-ui prerequisite.
build: build-joe

build-joe: build-ui
	go build -o joe ./cmd/joe

# Build the production web UI and stage it into the embed directory. Old
# staged output is cleared first (it is gitignored) while the committed
# .gitkeep — never wiped — keeps the git tree clean across builds.
build-ui:
	cd ui && npm ci && npm run build
	rm -rf $(EMBED_UI_DIR)/assets $(EMBED_UI_DIR)/index.html $(EMBED_UI_DIR)/vite.svg
	mkdir -p $(EMBED_UI_DIR)
	cp -R ui/dist/. $(EMBED_UI_DIR)/

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
