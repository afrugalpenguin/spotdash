// Command spotdash is the desk dashboard agent: it collects data from a set of
// pluggable sources and serves a web UI and a live feed over the LAN.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/logging"
	"github.com/afrugalpenguin/spotdash/agent/internal/server"
	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	configFileName = "config.json"
	logFileName    = "spotdash.log"
	shutdownGrace  = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spotdash: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config.json (default: next to the binary, then the working directory)")
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

	log.Info("starting",
		"version", version,
		"config", resolved,
		"log_file", logPath,
		"listen", cfg.Listen,
		"log_level", cfg.LogLevel,
	)

	store := state.New()
	for _, name := range cfg.SourceNames() {
		if cfg.Sources[name].Enabled {
			// Enabled but not yet producing data. The registry flips this to ok
			// on the first successful poll.
			store.Register(name, state.StatusDegraded, "awaiting first poll")
		} else {
			store.Register(name, state.StatusDisabled, "")
		}
		log.Debug("registered source", "source", name, "enabled", cfg.Sources[name].Enabled)
	}

	srv := server.New(server.Options{
		Token:   cfg.Token,
		Version: version,
		Started: time.Now(),
		Store:   store,
		Logger:  log,
	})

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
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

	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), configFileName))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, configFileName))
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
