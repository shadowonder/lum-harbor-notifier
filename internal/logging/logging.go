package logging

import (
	"fmt"
	"io"
	"log/slog"
	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/config"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// New creates a *slog.Logger based on the given logging configuration.
// It writes to stdout and/or a rotating log file (via lumberjack) depending on
// cfg.Console / cfg.File, applies the parsed log level to a text handler, and
// returns an error if no output destination is enabled.
func New(cfg config.LoggingConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	var writers []io.Writer

	if cfg.Console {
		writers = append(writers, os.Stdout)
	}

	if cfg.File {
		logDir, err := resolveLogDirectory(cfg.Directory)
		if err != nil {
			return nil, fmt.Errorf("resolve log directory: %w", err)
		}

		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}

		logPath := filepath.Join(logDir, cfg.FileName)
		fileWriter := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   cfg.Compress,
		}

		writers = append(writers, fileWriter)
	}

	if len(writers) == 0 {
		return nil, fmt.Errorf("logging requires at least one input")
	}

	writer := io.MultiWriter(writers...)

	handler := slog.NewTextHandler(
		writer,
		&slog.HandlerOptions{
			Level: level,
		},
	)

	return slog.New(handler), nil
}

// resolveLogDirectory determines the directory where log files should be stored.
// If dir is non-empty, it is used as-is; otherwise it falls back to
// "lum_harbor/logs" under the OS user config directory.
func resolveLogDirectory(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}

	configDir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "logs"), nil
}

// parseLevel converts a case-insensitive log level string into a slog.Level.
// Supported values are "debug", "info", "warn"/"warning", and "error"; an
// unsupported value returns slog.LevelInfo along with an error.
func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level: %s", value)
	}
}
