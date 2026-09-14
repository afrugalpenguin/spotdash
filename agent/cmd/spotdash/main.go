// Command spotdash is the desk dashboard agent: it collects data from a set of
// pluggable sources and serves a web UI and a live feed over the LAN.
//
// Everything about the lifecycle lives in the app package, which is drivable
// without a desktop session. This file is wiring: resolve the config path, set
// up logging, and hand over to either the tray or a signal wait.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/afrugalpenguin/spotdash/agent/internal/app"
	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/logging"
	"github.com/afrugalpenguin/spotdash/agent/internal/tray"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	configFileName = "config.json"
	logFileName    = "spotdash.log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spotdash: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config.json (default: next to the binary, then the working directory)")
	noTray := flag.Bool("no-tray", false, "run without the tray icon, for a console or a machine with no desktop session")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	resolved, err := resolveConfigPath(*configPath)
	if err != nil {
		return err
	}

	// Read once here only to configure logging. The app reads the file itself
	// and owns it from then on, including on reload.
	cfg, err := config.Load(resolved)
	if err != nil {
		return err
	}
	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	logPath := filepath.Join(filepath.Dir(resolved), logFileName)
	log, closeLog, err := logging.New(logging.Options{Level: level, FilePath: logPath})
	if err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}
	defer func() {
		if err := closeLog(); err != nil {
			fmt.Fprintf(os.Stderr, "spotdash: closing log file: %v\n", err)
		}
	}()

	log.Info("starting", "version", version, "config", resolved, "log_file", logPath)

	agent := app.New(resolved, version, log)
	if err := agent.Start(); err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	if *noTray {
		<-signals
		log.Info("shutdown requested")
		return finish(agent, log)
	}

	// Ctrl+C and tray Quit converge on the same path. A signal has to take the
	// tray down too, or systray keeps the process alive with nothing left to
	// serve.
	go func() {
		<-signals
		log.Info("shutdown requested")
		tray.Stop()
	}()

	// systray takes over this goroutine and expects to be on the main one, so
	// it goes last.
	tray.Run(tray.Options{
		Controller: agent,
		Version:    version,
		Log:        log,
	})

	return finish(agent, log)
}

func finish(agent *app.App, log *slog.Logger) error {
	if err := agent.Stop(); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}

// resolveConfigPath finds config.json. An explicit flag wins. Otherwise the
// directory holding the binary is searched first, then the working directory,
// so that both a built binary and `go run` behave sensibly.
func resolveConfigPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	// The binary usually sits in the working directory, in which case both
	// candidates are the same path and listing it twice in the error reads as a
	// bug in the message rather than a missing file.
	var candidates []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), configFileName))
	}
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, configFileName))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("cannot determine where to look for config.json: pass -config")
	}
	return "", fmt.Errorf("config file not found: looked in %v. Copy config.example.json to config.json, or pass -config", candidates)
}
