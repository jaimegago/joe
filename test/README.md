# Joe Testing Guide

## Test Levels

Joe uses a three-tier testing strategy:

### 1. Unit Tests (Fast)
- Location: `internal/*/` (next to source files)
- Focus: Individual functions and components
- Dependencies: None or minimal mocks
- Run: `make test-unit`
- Speed: < 5 seconds

### 2. Integration Tests (Moderate)
- Location: `test/integration/`
- Focus: Component interactions with mocks
- Dependencies: Mock LLM, in-memory store
- Run: `make test-integration`
- Speed: < 30 seconds

### 3. End-to-End Tests (Slow)
- Location: `test/e2e/`
- Focus: Full system with both binaries
- Dependencies: Building binaries, real processes
- Run: `make test-e2e`
- Speed: 1-5 minutes

## Quick Start

```bash
# Run everything (recommended before commits)
make test-all

# Run only fast tests during development
make test-unit

# Run integration tests when changing component interactions
make test-integration

# Run e2e tests before releases or when testing full flows
make test-e2e
```

## Writing Tests

### Unit Test Example

```go
func TestConfig_Load(t *testing.T) {
    // Test individual function
    cfg, err := config.Load("test-config.yaml")
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }
    // assertions...
}
```

### Integration Test Example

```go
// +build integration

package integration

func TestIntegration_Conversation(t *testing.T) {
    // Use mocks for external dependencies
    mockLLM := mocks.NewMockLLM()
    mockLLM.SetupScenario("simple_question")
    
    agent := useragent.New(mockLLM, tools.NewDefaultRegistry())
    
    // Test component interaction
    resp, err := agent.Run(context.Background(), "test message")
    // assertions...
}
```

### E2E Test Example

```go
// +build e2e

package e2e

func TestE2E_FullFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start: %v", err)
    }
    
    // Test real interaction
    response, err := harness.SendMessage("test message")
    // assertions...
}
```

## Test Organization

```
joe/
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go          # Unit tests
│   ├── useragent/
│   │   ├── agent.go
│   │   └── agent_test.go           # Unit tests with mocks
│   └── ...
├── test/
│   ├── integration/
│   │   ├── conversation_test.go    # Component interaction tests
│   │   └── api_store_test.go       # API + Store tests
│   ├── e2e/
│   │   ├── harness.go              # Test infrastructure
│   │   ├── basic_test.go           # Basic E2E scenarios
│   │   └── regression_test.go      # Known bug prevention
│   ├── mocks/
│   │   ├── llm.go                  # Mock LLM adapter
│   │   └── filesystem.go           # Mock file system
│   └── fixtures/
│       ├── configs/                # Test configs
│       └── conversations/          # Test data
```

## Mocking

### Mock LLM Adapter

```go
// Create mock
mockLLM := mocks.NewMockLLM()

// Setup predefined scenario
mockLLM.SetupScenario("simple_question")

// Or set custom responses
mockLLM.SetResponse("hello", llm.Response{
    Content: "Hi there!",
    Model: "mock-model",
})

// Verify calls
if mockLLM.CallCount != 2 {
    t.Errorf("expected 2 calls, got %d", mockLLM.CallCount)
}
```

### Mock File System

```go
// Create mock
mockFS := mocks.NewMockFileSystem()

// Setup files
mockFS.Files["config.yaml"] = "llm:\n  provider: gemini\n"

// Use in tools
tool := readfile.NewWithFS(mockFS)

// Verify operations
if len(mockFS.ReadCalls) != 1 {
    t.Error("expected 1 read operation")
}
```

## Coverage

Check coverage:

```bash
# Unit test coverage
make test-coverage-unit

# Integration test coverage
make test-coverage-integration

# View HTML report
open coverage.html
```

## CI/CD

Tests run automatically on push:

- Unit tests: Always run
- Integration tests: Always run
- E2E tests: Run on main branch and PRs

## Debugging

### Failed Unit Test
```bash
go test -v ./internal/config -run TestLoad
```

### Failed Integration Test
```bash
go test -v -tags=integration ./test/integration -run TestIntegration_Conversation
```

### Failed E2E Test
```bash
# E2E tests print logs on failure
make test-e2e

# Run specific test
go test -v -tags=e2e ./test/e2e -run TestE2E_StartupAndStatus
```

## Best Practices

1. **Use the right test level**
   - Unit: Testing logic and edge cases
   - Integration: Testing component interactions
   - E2E: Testing user workflows

2. **Keep tests fast**
   - Unit tests should run in milliseconds
   - Use mocks to avoid slow operations
   - Only use E2E for critical paths

3. **Make tests deterministic**
   - Use fixed timestamps in tests
   - Mock random generators
   - Clean up test data

4. **Test error paths**
   - Happy path is not enough
   - Test error handling
   - Test edge cases

5. **Document test intent**
   - Clear test names
   - Comments for complex setups
   - Link to relevant issues/docs

## Adding New Tests

### For New Feature

1. Add unit tests for new functions
2. Add integration test if feature integrates components
3. Add E2E test if feature is user-facing

### For Bug Fix

1. Add regression test in appropriate level
2. Tag with issue number in test name
3. Document what bug is being prevented

Example:
```go
func TestRegression_Issue123_ConfigReload(t *testing.T) {
    // Verify that config changes are detected without restart
    // See: https://github.com/jaimegago/joe/issues/123
}
```

## Common Issues

### "No tests to run"
- Check build tags: `-tags=integration` or `-tags=e2e`
- Check file naming: `*_test.go`

### "Connection refused" in E2E tests
- Ensure binaries are built: `make build`
- Check port conflicts (tests use 7778)
- Check firewall settings

### "Test timeout"
- E2E tests can be slow, increase timeout:
  ```bash
  go test -timeout 10m ./test/e2e/...
  ```

## References

- [Testing Strategy](../docs/testing-strategy.md) - Detailed strategy
- [Go Testing](https://go.dev/doc/tutorial/add-a-test) - Go test basics
- [Table Driven Tests](https://go.dev/wiki/TableDrivenTests) - Test patterns
