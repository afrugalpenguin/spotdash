package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevelKnownValues(t *testing.T) {
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"INFO":  slog.LevelInfo,
	}

	for in, want := range tests {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseLevelRejectsUnknown(t *testing.T) {
	_, err := ParseLevel("chatty")
	if err == nil {
		t.Fatal("ParseLevel(chatty) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "chatty") {
		t.Errorf("error = %v, want it to name chatty", err)
	}
}

func TestNewWritesToStderrAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelInfo, FilePath: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("hello from the test", "marker", "log-file-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(contents), "log-file-marker") {
		t.Errorf("log file missing the record:\n%s", contents)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelWarn, FilePath: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Debug("should not appear", "marker", "debug-marker")
	log.Warn("should appear", "marker", "warn-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	contents, _ := os.ReadFile(path)
	if strings.Contains(string(contents), "debug-marker") {
		t.Error("debug record written at warn level")
	}
	if !strings.Contains(string(contents), "warn-marker") {
		t.Error("warn record missing at warn level")
	}
}

// errWriter stands in for the stderr of a windowsgui build: every write fails.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, os.ErrInvalid }

func TestNewStillWritesTheFileWhenStderrFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelInfo, FilePath: path, Stderr: errWriter{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("serving", "marker", "no-console-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(contents), "no-console-marker") {
		t.Errorf("file missing the record when stderr fails:\n%s", contents)
	}
}

func TestLogFailureRecordsAnErrorLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	LogFailure(path, os.ErrPermission)

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, want := range []string{"level=ERROR", "startup failed", os.ErrPermission.Error()} {
		if !strings.Contains(string(contents), want) {
			t.Errorf("log missing %q:\n%s", want, contents)
		}
	}
}
