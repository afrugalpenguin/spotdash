// Package tray puts the agent in the system tray.
//
// It is deliberately thin. Everything it does is a call into the app package,
// which owns the lifecycle and is testable without a desktop session. This file
// is the part that cannot be tested automatically, so there is as little of it
// as possible.
package tray

import (
	_ "embed"
	"log/slog"
	"os/exec"
	"runtime"
)

//go:embed icon.ico
var iconICO []byte

// Controller is what the menu drives.
type Controller interface {
	Reload() error
	Stop() error
	OpenURL() string
	SettingsURL() string
}

// Options configures the tray.
type Options struct {
	Controller Controller
	Version    string
	Log        *slog.Logger
	// OnQuit runs after the tray has gone, so the caller can unblock whatever
	// is waiting for shutdown.
	OnQuit func()
}

// Icon is the tray image, exported so a caller can reuse it.
func Icon() []byte { return iconICO }

// openInBrowser launches the default browser.
//
// Windows needs the shell to resolve the default handler, and "start" is a
// cmd builtin rather than an executable, hence the indirection. The empty
// argument is the window title that start would otherwise take from a quoted
// URL.
func openInBrowser(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
