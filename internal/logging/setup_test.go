package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLoggerWithFile_WritesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "log.json")

	logger, cleanup := SetupLoggerWithFile(LevelInfo, logPath)
	logger.Info("hello", "key", "value")
	cleanup()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	logLine := string(content)
	if !strings.Contains(logLine, "\"msg\":\"hello\"") {
		t.Fatalf("expected log line to contain message, got: %s", logLine)
	}
	if !strings.Contains(logLine, "\"key\":\"value\"") {
		t.Fatalf("expected log line to contain key/value, got: %s", logLine)
	}
}

func TestSetupLoggerWithFile_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()

	logger, cleanup := SetupLoggerWithFile(LevelInfo, tmpDir)
	logger.Info("ignored")
	cleanup()
}

func TestSetupLogger_ReturnsLogger(t *testing.T) {
	logger := SetupLogger(LevelDebug)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}
