package debug

import (
	"log"
	"os"
	"path/filepath"
)

// Logger wraps stdlib log.Logger. When disabled, all writes are no-ops.
type Logger struct {
	inner *log.Logger
	file  *os.File
}

// NewLogger creates a debug logger. When enabled is false, the logger is a no-op.
// logPath is the file to write to (e.g., ~/.tmux-claude-matrix/logs/hooks.log).
func NewLogger(enabled bool, logPath string) (*Logger, error) {
	if !enabled {
		return &Logger{}, nil
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	return &Logger{
		inner: log.New(f, "", log.LstdFlags),
		file:  f,
	}, nil
}

// DefaultLogPath returns the default log file path.
func DefaultLogPath() string {
	return filepath.Join(os.Getenv("HOME"), ".tmux-claude-matrix/logs/hooks.log")
}

// Printf logs a formatted message. No-op if the logger is disabled or nil.
func (l *Logger) Printf(format string, v ...interface{}) {
	if l == nil || l.inner == nil {
		return
	}
	l.inner.Printf(format, v...)
}

// Close closes the underlying log file. Safe to call on nil or disabled loggers.
func (l *Logger) Close() {
	if l == nil || l.file == nil {
		return
	}
	l.file.Close()
}
