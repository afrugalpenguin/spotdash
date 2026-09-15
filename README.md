# spotdash

[![Go version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](agent/go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/afrugalpenguin/spotdash/agent)](https://goreportcard.com/report/github.com/afrugalpenguin/spotdash/agent)
[![License: MIT](https://img.shields.io/github/license/afrugalpenguin/spotdash)](LICENSE)

Inspired by [this $4 thrift-store Echo Spot rebuild](https://www.reddit.com/r/amazonecho/comments/1wen63x/i_turned_a_4_thrift_store_echo_spot_that_i_found/),
I picked up my own first-gen Amazon Echo Spot, put LineageOS on it, and
turned it into a desk dashboard. A Windows tray agent
collects machine telemetry, Spotify, and calendar data, serves a circular
480x480 web UI over the LAN, and pushes updates over a WebSocket. The Spot
just runs a kiosk WebView pointed at it.

All the logic lives in the agent. The Spot is just a panel.

## Faces

One face at a time, tap either half to switch.

<table>
<tr>
<td align="center" width="33%">
<img src="docs/screenshots/clock.png" width="220" alt="Clock face, analogue, with the next calendar event"><br>
Clock
</td>
<td align="center" width="33%">
<img src="docs/screenshots/calendar.png" width="220" alt="Calendar face"><br>
Calendar
</td>
<td align="center" width="33%">
<img src="docs/screenshots/spotify.png" width="220" alt="Spotify face"><br>
Spotify
</td>
</tr>
<tr>
<td align="center" width="33%">
<img src="docs/screenshots/telemetry.png" width="220" alt="Telemetry face"><br>
Telemetry
</td>
<td align="center" width="33%">
<img src="docs/screenshots/status.png" width="220" alt="Status face"><br>
Status (debug)
</td>
<td width="33%"></td>
</tr>
</table>

The clock can be digital or analogue and show the next calendar event or
not, independently. That plus per-face show/hide lives on a settings page
(tray icon: Options):

<p align="left">
<img src="docs/screenshots/settings.png" width="260" alt="Settings page">
</p>

## Layout

| Path     | What it is                                                   |
| -------- | ------------------------------------------------------------- |
| `agent/` | Go tray app. HTTP server, WebSocket, pluggable data sources.  |
| `shell/` | Kotlin Android app. One Activity, fullscreen WebView.          |
| `docs/`  | Architecture notes and a verification log.                    |

## Hardware

Built for a 2017 Echo Spot (`rook`) on LineageOS 18.1, 480x480 circular
screen, about 1 GB RAM. You don't need one to hack on this though: the UI
runs in a browser and the shell runs in a 480x480 emulator.

## Getting it running

| Tool                | For                        |
| -------------------- | --------------------------- |
| Go 1.26+             | the agent                   |
| JDK 17+, Android SDK | the shell                   |
| mingw-w64 gcc        | `go test -race` only        |

```powershell
cd agent
copy config.example.json config.json
# edit config.json, set a non-empty "token"
go run ./cmd/spotdash
```

Open `http://localhost:8765/?token=<your token>`. The agent won't start
without a token in `config.json` (which is gitignored, keep it that way).

For the shell, spin up the matching emulator and install the app:

```powershell
cd shell\tools
.\avd.ps1

cd ..
.\gradlew assembleDebug
adb install -r app\build\outputs\apk\debug\app-debug.apk
adb shell am start -n dev.spotdash.shell/.PanelActivity
```

Long-press the display for 3 seconds to enter the agent URL and token
(from the emulator that's `http://10.0.2.2:8765`). That's the only UI the
shell has, there's no other input on the real device.

Want to work on faces without running the agent at all? Open
`agent/web/dev.html` directly, it's got a toolbar for switching faces and
faking state.

## Spotify and calendar

Both sources have a `mock` mode for testing and a real mode:

- **Spotify**: `mode: "api"`, needs a Spotify app (Client ID only, PKCE, no
  secret) and a redirect URI of `http://127.0.0.1:8765/spotify/callback`.
  Then visit `/spotify/connect` to authorise.
- **Calendar**: `mode: "ics"`, just a published ICS feed URL (Outlook: Share
  calendar > Publish). Keep the link private, anyone with it can read your
  calendar.

Full config keys and setup steps are in `docs/architecture.md`.

## Running it automatically

```powershell
cd agent
.\tools\autostart.ps1 -Install
```

Registers a scheduled task at logon (tray icon included, not a service, a
service has no desktop session to put a tray on). `-Status` and
`-Uninstall` do what you'd expect.

## Security

Shared bearer token over plain HTTP on a trusted LAN, no TLS. That's a
known, accepted tradeoff for a desk toy on a home network, not an
oversight, see `docs/architecture.md` for the reasoning.

## More docs

- [`docs/architecture.md`](docs/architecture.md) - how it's put together, and the full source config reference.
- [`docs/verify.md`](docs/verify.md) - how each piece was tested.
- [`docs/device.md`](docs/device.md) - bringing up a real Echo Spot.
