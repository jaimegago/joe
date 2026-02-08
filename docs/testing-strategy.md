# Joe Testing Strategy

Comprehensive automated testing approach for integration and end-to-end functional testing.

---

## Overview

Joe uses a three-tier testing strategy:

1. **Unit Tests** (existing) - Test individual components in isolation
2. **Integration Tests** - Test component interactions with mocks (no external dependencies)
3. **End-to-End Tests** - Test full user workflows with both binaries running

---

## 1. Integration Tests (with Mocks)

**Goal**: Test component interactions without external dependencies (LLM APIs, real databases, external systems).

### Directory Structure

```
joe/
├── internal/
│   ├── api/
│   │   ├── server.go
│   │   └── server_test.go              # Unit tests
│   │   └── integration_test.go         # NEW: API integration tests
│   ├── useragent/
│   │   ├── agent.go
│   │   ├── agent_test.go              # Unit tests with mocks
│   │   └── integration_test.go         # NEW: Agent integration tests
│   └── ...
├── test/
│   ├── integration/                    # NEW: Integration test suite
│   │   ├── api_test.go                # API endpoint tests
│   │   ├── store_test.go              # Store operations
│   │   ├── conversation_test.go       # Full conversation flows
│   │   └── tools_test.go              # Tool execution
│   ├── mocks/                          # NEW: Mock implementations
│   │   ├── llm.go                     # Mock LLM adapter
│   │   ├── store.go                   # Mock store (or use in-memory)
│   │   └── tools.go                   # Mock tool executors
│   └── fixtures/                       # NEW: Test data
│       ├── conversations.json
│       ├── llm_responses.json
│       └── test_config.yaml
```

### Mock LLM Adapter

Create a predictable LLM adapter for testing:

```go
// test/mocks/llm.go
package mocks

import (
    "context"
    "github.com/jaimegago/joe/internal/llm"
)

type MockLLM struct {
    // Predefined responses mapped by conversation context
    Responses map[string]llm.Response
    CallCount int
    LastRequest llm.Request
}

func NewMockLLM() *MockLLM {
    return &MockLLM{
        Responses: make(map[string]llm.Response),
    }
}

func (m *MockLLM) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
    m.CallCount++
    m.LastRequest = req
    
    // Return canned response based on last user message
    lastMsg := req.Messages[len(req.Messages)-1].Content
    if resp, ok := m.Responses[lastMsg]; ok {
        return resp, nil
    }
    
    // Default response
    return llm.Response{
        Content: "Mock response",
        Model: "mock-model",
    }, nil
}

// Helper to set up common test scenarios
func (m *MockLLM) SetupScenario(scenario string) {
    switch scenario {
    case "simple_question":
        m.Responses["what is kubernetes?"] = llm.Response{
            Content: "Kubernetes is a container orchestration platform.",
            Model: "mock-model",
        }
    case "tool_call":
        m.Responses["read my config.yaml"] = llm.Response{
            Content: "I'll read that file for you.",
            ToolCalls: []llm.ToolCall{{
                Name: "read_file",
                Args: map[string]any{"path": "config.yaml"},
            }},
            Model: "mock-model",
        }
    case "multi_turn":
        m.Responses["hello"] = llm.Response{
            Content: "Hi! How can I help?",
            Model: "mock-model",
        }
        m.Responses["what can you do?"] = llm.Response{
            Content: "I can help with infrastructure tasks.",
            Model: "mock-model",
        }
    }
}
```

### Integration Test Examples

#### Test: API Endpoints

```go
// test/integration/api_test.go
package integration

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    
    "github.com/jaimegago/joe/internal/api"
    "github.com/jaimegago/joe/internal/store"
    "github.com/jaimegago/joe/test/mocks"
)

func TestAPI_StatusEndpoint(t *testing.T) {
    server := api.New()
    mux := http.NewServeMux()
    server.RegisterRoutes(mux)
    
    req := httptest.NewRequest("GET", "/api/v1/status", nil)
    w := httptest.NewRecorder()
    
    mux.ServeHTTP(w, req)
    
    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    
    var resp map[string]any
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }
    
    if resp["status"] != "ok" {
        t.Errorf("expected status=ok, got %v", resp["status"])
    }
}

func TestAPI_SourcesCRUD(t *testing.T) {
    // Setup in-memory store
    testStore, err := store.New(":memory:")
    if err != nil {
        t.Fatalf("failed to create test store: %v", err)
    }
    defer testStore.Close()
    
    if err := testStore.Migrate(); err != nil {
        t.Fatalf("failed to migrate: %v", err)
    }
    
    // TODO: Inject testStore into API server
    server := api.NewWithStore(testStore)
    mux := http.NewServeMux()
    server.RegisterRoutes(mux)
    
    // Test POST /api/v1/sources (create)
    source := map[string]any{
        "type": "kubernetes",
        "name": "prod-cluster",
        "config": map[string]any{"context": "prod"},
    }
    body, _ := json.Marshal(source)
    
    req := httptest.NewRequest("POST", "/api/v1/sources", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    
    mux.ServeHTTP(w, req)
    
    if w.Code != http.StatusCreated {
        t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
    }
    
    // Test GET /api/v1/sources (list)
    req = httptest.NewRequest("GET", "/api/v1/sources", nil)
    w = httptest.NewRecorder()
    
    mux.ServeHTTP(w, req)
    
    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    
    var sources []map[string]any
    if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
        t.Fatalf("failed to decode: %v", err)
    }
    
    if len(sources) != 1 {
        t.Errorf("expected 1 source, got %d", len(sources))
    }
}
```

#### Test: Conversation Flow with Mock LLM

```go
// test/integration/conversation_test.go
package integration

import (
    "context"
    "testing"
    
    "github.com/jaimegago/joe/internal/useragent"
    "github.com/jaimegago/joe/internal/tools"
    "github.com/jaimegago/joe/test/mocks"
)

func TestConversation_SimpleQuestion(t *testing.T) {
    // Setup
    mockLLM := mocks.NewMockLLM()
    mockLLM.SetupScenario("simple_question")
    
    registry := tools.NewDefaultRegistry()
    agent := useragent.New(mockLLM, registry)
    
    // Execute
    ctx := context.Background()
    resp, err := agent.Run(ctx, "what is kubernetes?")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    
    if resp != "Kubernetes is a container orchestration platform." {
        t.Errorf("unexpected response: %s", resp)
    }
    
    if mockLLM.CallCount != 1 {
        t.Errorf("expected 1 LLM call, got %d", mockLLM.CallCount)
    }
}

func TestConversation_WithToolCall(t *testing.T) {
    // Setup
    mockLLM := mocks.NewMockLLM()
    mockLLM.SetupScenario("tool_call")
    
    // After tool execution, mock the final response
    mockLLM.Responses["tool_result"] = llm.Response{
        Content: "The config.yaml contains your LLM settings.",
        Model: "mock-model",
    }
    
    // Mock file system for read_file tool
    mockFS := mocks.NewMockFileSystem()
    mockFS.Files["config.yaml"] = "llm:\n  provider: gemini\n"
    
    registry := tools.NewDefaultRegistry()
    registry.Register(tools.NewReadFileTool(mockFS))
    
    agent := useragent.New(mockLLM, registry)
    
    // Execute
    ctx := context.Background()
    resp, err := agent.Run(ctx, "read my config.yaml")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    
    if mockLLM.CallCount != 2 { // Initial + after tool call
        t.Errorf("expected 2 LLM calls, got %d", mockLLM.CallCount)
    }
    
    // Verify tool was called
    if len(mockFS.ReadCalls) != 1 {
        t.Errorf("expected 1 file read, got %d", len(mockFS.ReadCalls))
    }
}

func TestConversation_MultiTurn(t *testing.T) {
    mockLLM := mocks.NewMockLLM()
    mockLLM.SetupScenario("multi_turn")
    
    registry := tools.NewDefaultRegistry()
    agent := useragent.New(mockLLM, registry)
    session := useragent.NewSession()
    
    ctx := context.Background()
    
    // Turn 1
    resp1, err := agent.RunWithSession(ctx, session, "hello")
    if err != nil {
        t.Fatalf("turn 1 error: %v", err)
    }
    if resp1 != "Hi! How can I help?" {
        t.Errorf("unexpected turn 1 response: %s", resp1)
    }
    
    // Turn 2
    resp2, err := agent.RunWithSession(ctx, session, "what can you do?")
    if err != nil {
        t.Fatalf("turn 2 error: %v", err)
    }
    if resp2 != "I can help with infrastructure tasks." {
        t.Errorf("unexpected turn 2 response: %s", resp2)
    }
    
    // Verify session maintains history
    if len(session.Messages) != 4 { // 2 user + 2 assistant
        t.Errorf("expected 4 messages in session, got %d", len(session.Messages))
    }
}
```

#### Test: Tool Execution with Mocks

```go
// test/integration/tools_test.go
package integration

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    
    "github.com/jaimegago/joe/internal/tools"
    "github.com/jaimegago/joe/internal/tools/local/readfile"
    "github.com/jaimegago/joe/internal/tools/local/writefile"
)

func TestTools_FileOperations(t *testing.T) {
    // Setup temp directory
    tmpDir := t.TempDir()
    testFile := filepath.Join(tmpDir, "test.txt")
    
    registry := tools.NewRegistry()
    registry.Register(readfile.New())
    registry.Register(writefile.New())
    
    ctx := context.Background()
    
    // Test write_file
    writeResult, err := registry.Execute(ctx, "write_file", map[string]any{
        "path": testFile,
        "content": "Hello, Joe!",
    })
    if err != nil {
        t.Fatalf("write_file failed: %v", err)
    }
    
    // Verify file exists
    if _, err := os.Stat(testFile); os.IsNotExist(err) {
        t.Fatal("file was not created")
    }
    
    // Test read_file
    readResult, err := registry.Execute(ctx, "read_file", map[string]any{
        "path": testFile,
    })
    if err != nil {
        t.Fatalf("read_file failed: %v", err)
    }
    
    content := readResult.(string)
    if content != "Hello, Joe!" {
        t.Errorf("expected 'Hello, Joe!', got '%s'", content)
    }
}

func TestTools_GitOperations(t *testing.T) {
    // Setup: Create a git repo in temp directory
    tmpDir := t.TempDir()
    setupTestGitRepo(t, tmpDir)
    
    registry := tools.NewDefaultRegistry()
    ctx := context.Background()
    
    // Test git_status
    status, err := registry.Execute(ctx, "git_status", map[string]any{
        "path": tmpDir,
    })
    if err != nil {
        t.Fatalf("git_status failed: %v", err)
    }
    
    // Should show clean working tree
    if !contains(status.(string), "working tree clean") {
        t.Errorf("expected clean working tree, got: %s", status)
    }
    
    // Make a change
    testFile := filepath.Join(tmpDir, "README.md")
    os.WriteFile(testFile, []byte("# Test\nUpdated"), 0644)
    
    // Test git_diff
    diff, err := registry.Execute(ctx, "git_diff", map[string]any{
        "path": tmpDir,
    })
    if err != nil {
        t.Fatalf("git_diff failed: %v", err)
    }
    
    if !contains(diff.(string), "Updated") {
        t.Errorf("expected diff to show 'Updated', got: %s", diff)
    }
}
```

### Running Integration Tests

Add to `Makefile`:

```makefile
# Run integration tests only
test-integration:
	go test -v ./test/integration/... -tags=integration

# Run integration tests with coverage
test-integration-coverage:
	go test -v -cover ./test/integration/... -tags=integration

# Run unit tests only
test-unit:
	go test -v ./internal/... -short
```

---

## 2. End-to-End Functional Tests

**Goal**: Test full Joe functionality with both binaries running, simulating real user scenarios.

### Directory Structure

```
joe/
├── test/
│   ├── e2e/                            # NEW: E2E test suite
│   │   ├── setup.go                   # Test harness
│   │   ├── basic_test.go              # Basic flows
│   │   ├── conversation_test.go       # Multi-turn conversations
│   │   ├── config_test.go             # Configuration scenarios
│   │   └── regression_test.go         # Known bug scenarios
│   └── fixtures/
│       └── e2e/
│           ├── test_config.yaml
│           └── test_repos/
```

### E2E Test Harness

```go
// test/e2e/setup.go
package e2e

import (
    "bufio"
    "context"
    "fmt"
    "io"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
    "time"
)

// JoeTestHarness manages joe and joecored for testing
type JoeTestHarness struct {
    t              *testing.T
    joecored       *exec.Cmd
    joecoredLog    *os.File
    apiURL         string
    configPath     string
    tmpDir         string
    cleanupFuncs   []func()
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T) *JoeTestHarness {
    tmpDir := t.TempDir()
    
    // Setup test config
    configPath := filepath.Join(tmpDir, "config.yaml")
    setupTestConfig(t, configPath)
    
    return &JoeTestHarness{
        t:          t,
        apiURL:     "http://localhost:7778", // Use different port for tests
        configPath: configPath,
        tmpDir:     tmpDir,
    }
}

// Start starts joecored and waits for it to be ready
func (h *JoeTestHarness) Start() error {
    // Build joecored if needed
    if err := h.buildBinaries(); err != nil {
        return fmt.Errorf("build failed: %w", err)
    }
    
    // Start joecored
    logFile, err := os.Create(filepath.Join(h.tmpDir, "joecored.log"))
    if err != nil {
        return err
    }
    h.joecoredLog = logFile
    
    h.joecored = exec.Command("./joecored")
    h.joecored.Env = append(os.Environ(),
        fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
        "JOE_SERVER_ADDRESS=localhost:7778",
        "JOE_LOG_LEVEL=debug",
        "GEMINI_API_KEY=test-key-for-e2e", // Use test key or mock server
    )
    h.joecored.Stdout = logFile
    h.joecored.Stderr = logFile
    
    if err := h.joecored.Start(); err != nil {
        return fmt.Errorf("failed to start joecored: %w", err)
    }
    
    h.t.Logf("Started joecored (PID: %d)", h.joecored.Process.Pid)
    
    // Wait for API to be ready
    if err := h.waitForAPI(30 * time.Second); err != nil {
        h.Stop()
        return err
    }
    
    return nil
}

// Stop stops joecored and cleans up
func (h *JoeTestHarness) Stop() {
    if h.joecored != nil && h.joecored.Process != nil {
        h.t.Logf("Stopping joecored (PID: %d)", h.joecored.Process.Pid)
        h.joecored.Process.Kill()
        h.joecored.Wait()
    }
    
    if h.joecoredLog != nil {
        h.joecoredLog.Close()
        
        // Print logs on failure
        if h.t.Failed() {
            h.t.Log("=== joecored logs ===")
            content, _ := os.ReadFile(h.joecoredLog.Name())
            h.t.Log(string(content))
        }
    }
    
    for _, cleanup := range h.cleanupFuncs {
        cleanup()
    }
}

// RunCommand runs joe CLI command and returns output
func (h *JoeTestHarness) RunCommand(args ...string) (string, error) {
    cmd := exec.Command("./joe", args...)
    cmd.Env = append(os.Environ(),
        fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
        "JOE_SERVER_ADDRESS=localhost:7778",
    )
    
    output, err := cmd.CombinedOutput()
    return string(output), err
}

// SendMessage sends a message through the API
func (h *JoeTestHarness) SendMessage(message string) (string, error) {
    // TODO: Implement API client call
    // POST /api/v1/chat with message, return response
    return "", nil
}

// waitForAPI polls the status endpoint until it responds
func (h *JoeTestHarness) waitForAPI(timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return fmt.Errorf("timeout waiting for API to start")
        case <-ticker.C:
            resp, err := http.Get(fmt.Sprintf("%s/api/v1/status", h.apiURL))
            if err == nil && resp.StatusCode == http.StatusOK {
                h.t.Log("API is ready")
                return nil
            }
        }
    }
}

func (h *JoeTestHarness) buildBinaries() error {
    cmd := exec.Command("make", "build")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("build failed: %s", output)
    }
    return nil
}

func setupTestConfig(t *testing.T, path string) {
    config := `
llm:
  provider: gemini
  current_model: gemini-2.0-flash
  available_models:
    - gemini-2.0-flash

server:
  address: localhost:7778

logging:
  level: debug
`
    if err := os.WriteFile(path, []byte(config), 0644); err != nil {
        t.Fatalf("failed to write test config: %v", err)
    }
}
```

### E2E Test Examples

```go
// test/e2e/basic_test.go
package e2e

import (
    "testing"
    "time"
)

func TestE2E_StartupAndStatus(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start harness: %v", err)
    }
    
    // API should be responding
    // This is tested by Start(), so we're done
    t.Log("✓ Joe started successfully")
}

func TestE2E_SimpleConversation(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start: %v", err)
    }
    
    // Send a message
    resp, err := harness.SendMessage("hello")
    if err != nil {
        t.Fatalf("failed to send message: %v", err)
    }
    
    // Should get a response
    if len(resp) == 0 {
        t.Error("expected non-empty response")
    }
    
    t.Logf("Response: %s", resp)
}

func TestE2E_ConfigurationLoading(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start: %v", err)
    }
    
    // Query current configuration
    output, err := harness.RunCommand("config", "show")
    if err != nil {
        t.Fatalf("failed to get config: %v", err)
    }
    
    // Should show test configuration
    if !contains(output, "gemini-2.0-flash") {
        t.Errorf("expected test config, got: %s", output)
    }
}
```

```go
// test/e2e/conversation_test.go
package e2e

import (
    "testing"
)

func TestE2E_MultiTurnConversation(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start: %v", err)
    }
    
    tests := []struct {
        message      string
        expectContains string
    }{
        {"hello", ""},
        {"what can you help me with?", "infrastructure"},
        {"list available tools", "read_file"},
    }
    
    for i, tt := range tests {
        t.Run(fmt.Sprintf("turn_%d", i+1), func(t *testing.T) {
            resp, err := harness.SendMessage(tt.message)
            if err != nil {
                t.Fatalf("turn %d failed: %v", i+1, err)
            }
            
            if tt.expectContains != "" && !contains(resp, tt.expectContains) {
                t.Errorf("expected response to contain %q, got: %s", 
                    tt.expectContains, resp)
            }
            
            t.Logf("Turn %d - User: %s", i+1, tt.message)
            t.Logf("Turn %d - Joe: %s", i+1, resp)
        })
    }
}

func TestE2E_ToolExecution(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    harness := NewTestHarness(t)
    defer harness.Stop()
    
    if err := harness.Start(); err != nil {
        t.Fatalf("failed to start: %v", err)
    }
    
    // Create a test file
    testFile := filepath.Join(harness.tmpDir, "test.txt")
    os.WriteFile(testFile, []byte("test content"), 0644)
    
    // Ask Joe to read it
    resp, err := harness.SendMessage(fmt.Sprintf("read the file %s", testFile))
    if err != nil {
        t.Fatalf("failed: %v", err)
    }
    
    // Response should mention the file content
    if !contains(resp, "test content") {
        t.Errorf("expected response to include file content, got: %s", resp)
    }
}
```

```go
// test/e2e/regression_test.go
package e2e

import (
    "testing"
)

// TestE2E_RegressionIssueXXX tests for specific bugs that were fixed
func TestE2E_RegressionIssue42_ConfigReload(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    // Test that config changes are detected without restart
    // Add specific regression test scenarios as bugs are found and fixed
}

func TestE2E_RegressionIssue57_LongRunningConversation(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test in short mode")
    }
    
    // Test that long conversations don't cause memory leaks
    // or session corruption
}
```

### Running E2E Tests

Add to `Makefile`:

```makefile
# Run e2e tests (requires building binaries)
test-e2e: build
	go test -v ./test/e2e/... -timeout 5m

# Run e2e tests with logs
test-e2e-verbose: build
	go test -v ./test/e2e/... -timeout 5m -args -test.v

# Run all tests (unit + integration + e2e)
test-all: test-unit test-integration test-e2e
```

---

## 3. Test Environments

### Local Development

```bash
# Unit tests (fast, no mocks needed)
make test-unit

# Integration tests (with mocks, no external deps)
make test-integration

# E2E tests (full system, requires build)
make test-e2e

# Everything
make test-all
```

### CI/CD Pipeline

```yaml
# .github/workflows/test.yml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run unit tests
        run: make test-unit

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run integration tests
        run: make test-integration

  e2e-tests:
    runs-on: ubuntu-latest
    env:
      GEMINI_API_KEY: ${{ secrets.TEST_GEMINI_API_KEY }}
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Build binaries
        run: make build
      - name: Run e2e tests
        run: make test-e2e
```

---

## 4. Testing Best Practices

### Test Organization

1. **Unit tests**: `*_test.go` next to source files
2. **Integration tests**: `test/integration/`
3. **E2E tests**: `test/e2e/`
4. **Mocks**: `test/mocks/`
5. **Fixtures**: `test/fixtures/`

### Test Naming

```go
// Unit tests
func TestComponentName_Method_Scenario(t *testing.T) {}

// Integration tests
func TestIntegration_Component_Flow(t *testing.T) {}

// E2E tests
func TestE2E_UserScenario_Description(t *testing.T) {}
```

### Test Tags

```go
// +build integration
// Integration tests with mocks

// +build e2e
// End-to-end tests requiring full system
```

### Coverage Goals

- **Unit tests**: > 80% coverage per package
- **Integration tests**: Critical user flows
- **E2E tests**: Happy paths + known regressions

---

## 5. Implementation Checklist

### Phase 1: Integration Tests Setup
- [ ] Create `test/` directory structure
- [ ] Implement mock LLM adapter
- [ ] Implement mock file system
- [ ] Add integration test for API endpoints
- [ ] Add integration test for conversation flow
- [ ] Add integration test for tool execution
- [ ] Update Makefile with `test-integration` target

### Phase 2: E2E Tests Setup
- [ ] Implement test harness (start/stop binaries)
- [ ] Add basic e2e test (startup + status)
- [ ] Add e2e test for simple conversation
- [ ] Add e2e test for configuration loading
- [ ] Add e2e test for multi-turn conversation
- [ ] Add e2e test for tool execution
- [ ] Update Makefile with `test-e2e` target

### Phase 3: CI/CD Integration
- [ ] Add GitHub Actions workflow
- [ ] Configure test secrets (API keys)
- [ ] Set up test result reporting
- [ ] Add coverage reporting

### Phase 4: Documentation & Maintenance
- [ ] Document testing strategy (this doc)
- [ ] Add testing section to README
- [ ] Create regression test template
- [ ] Set up test data fixtures

---

## 6. Maintenance

### Adding New Tests

1. **Bug found?** → Add regression test in `test/e2e/regression_test.go`
2. **New feature?** → Add integration tests for component interactions
3. **New component?** → Add unit tests + integration tests

### Test Data Management

- Store test fixtures in `test/fixtures/`
- Use JSON/YAML for structured data
- Use `testdata/` subdirectories for file-based tests
- Keep test data small and focused

### Debugging Failed Tests

```bash
# Run specific test with verbose output
go test -v ./test/e2e -run TestE2E_MultiTurn

# Run with race detector
go test -race ./test/integration/...

# Run with coverage
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Summary

This testing strategy provides:

1. **Fast feedback**: Unit tests run in seconds
2. **Confident refactoring**: Integration tests verify component interactions
3. **Regression prevention**: E2E tests catch real-world issues
4. **No flakiness**: Mocks eliminate external dependencies in integration tests
5. **CI-ready**: All tests can run in automated pipelines

The three-tier approach ensures comprehensive coverage while maintaining fast iteration cycles for development.
