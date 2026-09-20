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
			t.Errorf("ParseLevel(%q) returned an error: %v", in, err)
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
		t.Fatal("ParseLevel should reject an unknown level")
	}
	if !strings.Contains(err.Error(), "chatty") {
		t.Errorf("error should name the bad value, got: %v", err)
	}
}

func TestNewWritesToStderrAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelInfo, FilePath: path})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	log.Info("hello from the test", "marker", "log-file-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("closing the logger: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the log file should exist next to the binary: %v", err)
	}
	if !strings.Contains(string(contents), "log-file-marker") {
		t.Errorf("log file does not contain the logged record:\n%s", contents)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelWarn, FilePath: path})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	log.Debug("should not appear", "marker", "debug-marker")
	log.Warn("should appear", "marker", "warn-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("closing the logger: %v", err)
	}

	contents, _ := os.ReadFile(path)
	if strings.Contains(string(contents), "debug-marker") {
		t.Error("a debug record was written at warn level")
	}
	if !strings.Contains(string(contents), "warn-marker") {
		t.Error("a warn record was not written at warn level")
	}
}

// errWriter stands in for the stderr of a process with no console, which is
// what a windowsgui build has: every write to it fails.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, os.ErrInvalid }

func TestNewStillWritesTheFileWhenStderrFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	log, closeFn, err := New(Options{Level: slog.LevelInfo, FilePath: path, Stderr: errWriter{}})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	log.Info("serving", "marker", "no-console-marker")
	if err := closeFn(); err != nil {
		t.Fatalf("closing the logger: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the log file should exist: %v", err)
	}
	if !strings.Contains(string(contents), "no-console-marker") {
		t.Errorf("a dead stderr stopped the record reaching the file:\n%s", contents)
	}
}

func TestLogFailureRecordsAnErrorLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotdash.log")

	LogFailure(path, os.ErrPermission)

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("LogFailure should have created the log file: %v", err)
	}
	for _, want := range []string{"level=ERROR", "startup failed", os.ErrPermission.Error()} {
		if !strings.Contains(string(contents), want) {
			t.Errorf("log should contain %q:\n%s", want, contents)
		}
	}
}
