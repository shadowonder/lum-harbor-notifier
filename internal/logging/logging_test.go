package logging

import (
	"lum-harbor-notifier/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesLogToFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.LoggingConfig{
		Level:      "debug",
		Console:    false,
		File:       true,
		Directory:  tmpDir,
		FileName:   "test.log",
		MaxSizeMB:  1,
		MaxBackups: 2,
		MaxAgeDays: 1,
		Compress:   false,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	logger.Info("Database opened", "component", "database")

	logPath := filepath.Join(tmpDir, cfg.FileName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	logText := string(data)

	if !strings.Contains(logText, "Database opened") {
		t.Fatalf("expected log message to be written, got: %s", logText)
	}

	if !strings.Contains(logText, "component=database") {
		t.Fatalf("expected structured field to be written, got: %s", logText)
	}
}

func TestNewRejectsConfigWithoutOutput(t *testing.T) {
	cfg := config.LoggingConfig{
		Level:   "info",
		Console: false,
		File:    false,
	}

	logger, err := New(cfg)

	if err == nil {
		t.Fatal("expected an error when both console and file logging are disabled")
	}

	if logger != nil {
		t.Fatal("expected logger to be nil when configuration is invalid")
	}
}
