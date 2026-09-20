// Package tray puts the agent in the system tray. It is thin: everything it does
// is a call into the app package.
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
	// Autostart backs the "Start with Windows" item. Nil leaves the item out,
	// for a platform with nothing to offer.
	Autostart Autostart
	// LogPath is the agent's log file, opened by the "View log" item. Empty
	// means there is no file to show.
	LogPath string
	// OnQuit runs after the tray has gone, so the caller can unblock whatever
	// is waiting for shutdown.
	OnQuit func()
}

// Icon is the tray image, exported so a caller can reuse it.
func Icon() []byte { return iconICO }

// OpenInBrowser launches the default browser, for a caller outside the tray such
// as a first run that has just created its config.
func OpenInBrowser(target string) error { return openInBrowser(target) }

// openInBrowser launches the default browser. On Windows "start" is a cmd
// builtin, and the empty argument stops it reading the URL as a window title.
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

// openLog shows the log file in a text viewer.
func openLog(path string) error {
	name, args := logCommand(runtime.GOOS, path)
	return exec.Command(name, args...).Start()
}

// logCommand is the command that shows path on the given OS. It skips the
// default handler because start on an unassociated .log raises "Open With".
func logCommand(goos, path string) (string, []string) {
	switch goos {
	case "windows":
		return "notepad.exe", []string{path}
	case "darwin":
		return "open", []string{"-t", path}
	default:
		return "xdg-open", []string{path}
	}
}
