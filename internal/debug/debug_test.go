package debug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledLogger_Writes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "debug-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "hooks.log")
	logger, err := NewLogger(true, logPath)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Printf("test message %s", "hello")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "test message hello") {
		t.Errorf("log file should contain message, got: %q", content)
	}
}

func TestDisabledLogger_NoOp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "debug-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "hooks.log")
	logger, err := NewLogger(false, logPath)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Printf("this should not appear")

	_, err = os.Stat(logPath)
	if err == nil {
		t.Error("log file should not be created when debug is disabled")
	}
}

func TestNilLoggerSafety(t *testing.T) {
	// A nil *Logger should not panic
	var logger *Logger
	logger.Printf("should not panic")
	logger.Close()
}

func TestLoggerCreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "debug-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "nested", "dir", "hooks.log")
	logger, err := NewLogger(true, logPath)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Printf("test")

	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("expected log file to exist at nested path, got: %v", err)
	}
}
