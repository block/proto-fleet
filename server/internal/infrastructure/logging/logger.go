package logging

import (
	"log/slog"
	"os"
	"sync"
)

type Config struct {
	Level slog.Level `help:"Log level" default:"debug" env:"LEVEL"`
	JSON  bool       `help:"Log level" default:"false" env:"JSON"`
}

// defaultBuffer is the process-global ring buffer that ServerLogService
// reads from. It's set by InitLogger and accessed via DefaultBuffer().
//
// Stored behind a mutex rather than left zero-valued so handlers can fail
// loudly (in tests, especially) when accessed before InitLogger ran.
var (
	defaultBufferMu sync.RWMutex
	defaultBuffer   *Buffer
)

// DefaultBuffer returns the process-global log buffer, or nil if InitLogger
// has not been called yet.
func DefaultBuffer() *Buffer {
	defaultBufferMu.RLock()
	defer defaultBufferMu.RUnlock()
	return defaultBuffer
}

// InitLogger configures slog with two handlers in tandem:
//
//  1. The standard text/JSON handler writing to stdout, identical to the
//     prior behavior.
//  2. An in-memory ring buffer that the log-viewer UI reads via
//     ServerLogService.ListServerLogs.
//
// Returns the buffer so callers can pass it explicitly to handlers that
// need to read it. The same buffer is also accessible via DefaultBuffer().
//
// AddSource is enabled so the buffer can show file:line for each entry.
func InitLogger(config Config) *Buffer {
	logOptions := &slog.HandlerOptions{
		Level:     config.Level,
		AddSource: true,
	}

	var stdoutHandler slog.Handler
	if config.JSON {
		stdoutHandler = slog.NewJSONHandler(os.Stdout, logOptions)
	} else {
		stdoutHandler = slog.NewTextHandler(os.Stdout, logOptions)
	}

	buf := NewBuffer(DefaultBufferCapacity, config.Level)

	defaultBufferMu.Lock()
	defaultBuffer = buf
	defaultBufferMu.Unlock()

	slog.SetDefault(slog.New(newTeeHandler(stdoutHandler, buf)))

	return buf
}
