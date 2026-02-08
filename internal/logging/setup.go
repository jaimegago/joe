package logging

import (
	"io"
	"log/slog"
	"os"
)

// SetupLogger creates a structured logger based on the provided log level.
// Supported levels: LevelDebug, LevelInfo, LevelWarn, LevelError
// Returns a configured slog.Logger using text output to stdout.
func SetupLogger(level string) *slog.Logger {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: ParseLevel(level),
	})
	return slog.New(handler)
}

// SetupLoggerWithFile creates a structured logger that writes to a file or discards output.
// If logFile is empty, output is discarded (useful for keeping REPL clean).
// If logFile is specified, logs are written as JSON to that file.
// Returns the logger and a cleanup function that must be called to close the file.
func SetupLoggerWithFile(level, logFile string) (*slog.Logger, func()) {
	lvl := ParseLevel(level)

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	var handler slog.Handler
	var cleanup func() = func() {} // No-op by default

	if logFile != "" {
		// Log to file
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Fall back to discarding if file open fails
			handler = slog.NewTextHandler(io.Discard, opts)
		} else {
			handler = slog.NewJSONHandler(file, opts)
			cleanup = func() { file.Close() }
		}
	} else {
		// No log file configured - discard logs to keep REPL clean
		handler = slog.NewTextHandler(io.Discard, opts)
	}

	return slog.New(handler), cleanup
}
