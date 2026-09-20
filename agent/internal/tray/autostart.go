package tray

import (
	"log/slog"

	"fyne.io/systray"
)

// Autostart is the "Start with Windows" setting the menu drives.
type Autostart interface {
	// Enabled is read from the source of truth each time, never remembered.
	Enabled() (bool, error)
	Set(on bool) error
	// Unavailable is why the setting cannot be turned on, or "".
	Unavailable() string
}

const autostartLabel = "Start with Windows"

// autostartTitle is the menu text. An unavailable setting carries its reason in
// the title so a greyed-out item explains itself.
func autostartTitle(unavailable string) string {
	if unavailable == "" {
		return autostartLabel
	}
	return autostartLabel + " (unavailable: " + unavailable + ")"
}

// syncAutostart makes the tick match what is actually set.
func syncAutostart(item *systray.MenuItem, a Autostart, log *slog.Logger) {
	enabled, err := a.Enabled()
	if err != nil {
		log.Warn("could not read the start with Windows setting", "error", err)
		return
	}
	if enabled && !item.Checked() {
		item.Check()
	} else if !enabled && item.Checked() {
		item.Uncheck()
	}
}

// toggleAutostart flips the setting. It reads the current state itself because
// the platform may already have flipped the tick.
func toggleAutostart(item *systray.MenuItem, a Autostart, log *slog.Logger) {
	enabled, err := a.Enabled()
	if err != nil {
		log.Warn("could not read the start with Windows setting", "error", err)
		return
	}
	if err := a.Set(!enabled); err != nil {
		log.Error("could not change the start with Windows setting", "error", err)
	} else if enabled {
		log.Info("start with Windows turned off")
	} else {
		log.Info("start with Windows turned on")
	}
	syncAutostart(item, a, log)
}
