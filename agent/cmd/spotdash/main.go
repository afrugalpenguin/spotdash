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

	agentroot "github.com/afrugalpenguin/spotdash/agent"
	"github.com/afrugalpenguin/spotdash/agent/internal/app"
	"github.com/afrugalpenguin/spotdash/agent/internal/autostart"
	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/instance"
	"github.com/afrugalpenguin/spotdash/agent/internal/logging"
	"github.com/afrugalpenguin/spotdash/agent/internal/tray"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const logFileName = "spotdash.log"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spotdash: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config.json (default: next to the binary, then the working directory, then the per-user folder, which a first run creates)")
	showConfigPath := flag.Bool("config-path", false, "print the config.json this agent would use and exit, creating nothing")
	noTray := flag.Bool("no-tray", false, "run without the tray icon, for a console or a machine with no desktop session")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	loc, err := locateConfig(*configPath)
	if err != nil {
		logging.LogFailure(filepath.Join(binaryDir(), logFileName), err)
		return err
	}
	if *showConfigPath {
		printLine(loc.Path)
		return nil
	}

	// No config anywhere and no -config: this is a first run, so make one. Only
	// ever for a clear "not there": a config that exists but is broken is the
	// person's to fix, and is never replaced. A failure here is still written to
	// the log file, since a build with no console has nowhere else to say it.
	created := false
	if !loc.Exists && *configPath == "" {
		switch err := config.CreateDefault(loc.Path, agentroot.ExampleConfig); {
		case err == nil:
			created = true
		case errors.Is(err, os.ErrExist):
			// Another copy created it a moment ago. Use theirs.
		default:
			err = fmt.Errorf("creating a first-run config at %s: %w", loc.Path, err)
			logging.LogFailure(filepath.Join(binaryDir(), logFileName), err)
			return err
		}
	}
	resolved := loc.Path
	logPath := filepath.Join(filepath.Dir(resolved), logFileName)

	// Read once here only to configure logging. The app reads the file itself
	// and owns it from then on, including on reload.
	cfg, err := config.Load(resolved)
	if err != nil {
		logging.LogFailure(logPath, err)
		return err
	}
	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		logging.LogFailure(logPath, err)
		return err
	}

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
	if created {
		log.Info("config created", "path", resolved)
	}

	// Started at login and again by hand, two agents would only find out when the
	// second failed to bind the port. Say why instead, and leave quietly: exit
	// code 0, so a scheduled task set to restart on failure does not respawn it.
	lockKey := resolved
	if abs, err := filepath.Abs(resolved); err == nil {
		lockKey = abs
	}
	release, first, err := instance.Acquire(lockKey)
	switch {
	case err != nil:
		log.Warn("could not check whether another spotdash is running, carrying on", "error", err)
	case !first:
		log.Info("another spotdash is already running for this config, exiting", "config", lockKey)
		return nil
	default:
		defer release()
	}

	agent := app.New(resolved, version, log)
	if err := agent.Start(); err != nil {
		log.Error("startup failed", "error", err)
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	if *noTray {
		<-signals
		log.Info("shutdown requested")
		return finish(agent, log)
	}

	// A first run has nothing to show until the panel is open, and the address
	// with its token is only known now, so open it for them.
	if created {
		if err := tray.OpenInBrowser(agent.OpenURL()); err != nil {
			log.Warn("could not open the browser", "error", err)
		}
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
		LogPath:    logPath,
		Autostart:  autostartOption(),
	})

	return finish(agent, log)
}

// binaryDir is the directory holding the running binary, or the working
// directory if that cannot be found.
func binaryDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// autostartOption is the "Start with Windows" setting for the tray, or nil
// where there is nothing to offer (not Windows, or the binary cannot be found).
func autostartOption() tray.Autostart {
	store := autostart.SystemStore()
	if store == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return autostart.New(store, exe, os.TempDir())
}

func finish(agent *app.App, log *slog.Logger) error {
	if err := agent.Stop(); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}

// locateConfig finds config.json: see config.Resolve for the order. Each
// directory is best effort, since an unknown one only removes a place to look.
func locateConfig(flagValue string) (config.Location, error) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	workDir, _ := os.Getwd()
	userDir, _ := os.UserConfigDir()
	return config.Resolve(flagValue, exeDir, workDir, userDir)
}
