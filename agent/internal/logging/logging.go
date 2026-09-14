// Package logging builds the agent logger: structured records written to both
// stderr and a rotating file next to the binary.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Options configures the logger.
type Options struct {
	Level    slog.Level
	FilePath string
	// Stderr defaults to os.Stderr. Tests override it.
	Stderr io.Writer
}

// ParseLevel maps a config log_level string onto an slog level.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q, want debug, info, warn, or error", name)
	}
}

// New returns a logger writing to stderr and to a rotating file, along with a
// function that flushes and closes the file.
func New(opts Options) (*slog.Logger, func() error, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	writers := []io.Writer{stderr}
	closeFn := func() error { return nil }

	if opts.FilePath != "" {
		rotator := &lumberjack.Logger{
			Filename:   opts.FilePath,
			MaxSize:    5, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   false,
		}
		writers = append(writers, rotator)
		closeFn = rotator.Close
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{
		Level: opts.Level,
	})
	return slog.New(handler), closeFn, nil
}
