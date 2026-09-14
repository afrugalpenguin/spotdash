package tray

import (
	"log/slog"

	"fyne.io/systray"
)

// Run shows the tray icon and blocks until Quit is chosen or Stop is called.
//
// systray.Run takes over the calling goroutine and expects to be on the main
// one, so the caller does this last.
func Run(opts Options) {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	systray.Run(func() { onReady(opts, log) }, func() {
		if opts.OnQuit != nil {
			opts.OnQuit()
		}
	})
}

// Stop takes the tray down from anywhere, which is how a Ctrl+C in the console
// unblocks Run.
func Stop() {
	systray.Quit()
}

func onReady(opts Options, log *slog.Logger) {
	systray.SetIcon(iconICO)
	systray.SetTitle("spotdash")
	systray.SetTooltip("spotdash " + opts.Version)

	openItem := systray.AddMenuItem("Open UI", "Open the dashboard in your browser")
	optionsItem := systray.AddMenuItem("Options", "Change panel settings, such as the accent colour")
	reloadItem := systray.AddMenuItem("Reload config", "Re-read config.json")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Stop the agent")

	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				target := opts.Controller.OpenURL()
				if target == "" {
					log.Warn("cannot open the UI, the agent is not serving")
					continue
				}
				if err := openInBrowser(target); err != nil {
					log.Error("could not open the browser", "error", err)
				}

			case <-optionsItem.ClickedCh:
				target := opts.Controller.SettingsURL()
				if target == "" {
					log.Warn("cannot open settings, the agent is not serving")
					continue
				}
				if err := openInBrowser(target); err != nil {
					log.Error("could not open the browser", "error", err)
				}

			case <-reloadItem.ClickedCh:
				// A failed reload is reported and the agent carries on with the
				// configuration it already has, so there is nothing to do here
				// but say so.
				if err := opts.Controller.Reload(); err != nil {
					log.Error("reload failed, still running the previous configuration", "error", err)
					continue
				}
				log.Info("configuration reloaded from the tray")

			case <-quitItem.ClickedCh:
				log.Info("quit chosen from the tray")
				systray.Quit()
				return
			}
		}
	}()
}
