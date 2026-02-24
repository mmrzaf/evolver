package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmrzaf/evolver/internal/config"
)

// Configure sets the process-wide default logger using the supplied config.
func Configure(cfg config.Logging) (func() error, error) {
	writers := []io.Writer{os.Stderr}
	closeFn := func() error { return nil }

	if path := strings.TrimSpace(cfg.File); path != "" {
		if err := ensureParentDir(path); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, f)
		closeFn = f.Close
	}

	handlerOpts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}
	output := io.MultiWriter(writers...)

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "json":
		handler = slog.NewJSONHandler(output, handlerOpts)
	default:
		handler = slog.NewTextHandler(output, handlerOpts)
	}

	slog.SetDefault(slog.New(handler))
	return closeFn, nil
}

func parseLevel(level string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}
	return nil
}
