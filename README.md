# spotdash

A desk dashboard. A Windows tray agent collects machine telemetry and other data,
serves a circular 480x480 web UI over the LAN, and pushes live updates over a
WebSocket. A minimal Android app runs as a fullscreen kiosk launcher on an
Amazon Echo Spot and displays that UI in a WebView.

All logic and all UI live in the agent. The display device is a dumb panel.

## Components

| Path     | What it is                                                                 |
| -------- | -------------------------------------------------------------------------- |
| `agent/` | Go tray application. HTTP server, WebSocket, pluggable data sources, embedded web UI. |
| `shell/` | Kotlin Android app. Single Activity, immersive WebView, HOME launcher.      |
| `docs/`  | Architecture notes and the verification log.                               |

## Target hardware

The display is a 1st-generation Amazon Echo Spot (2017, codename `rook`) running
LineageOS 18.1 (Android 11, API 30). It has a 480x480 circular screen, roughly
1 GB of RAM, and a MediaTek MT8163 SoC. The shell does as little as possible so
the device can keep up.

Neither component requires that device to develop against. The UI runs in a
desktop browser and the shell runs in a 480x480 Android emulator.

## Prerequisites

| Tool                | Needed for                                                |
| ------------------- | --------------------------------------------------------- |
| Go 1.25 or later    | The agent.                                                |
| mingw-w64 gcc       | `go test -race` only. Nothing shipped needs it.           |
| JDK 17 or later     | The shell.                                                |
| Android SDK, API 30 | The shell and its emulator.                               |

The agent itself needs no C compiler: GPU telemetry binds `nvml.dll` directly in
pure Go, so the build is cgo free and the result is a single static binary. A
compiler is only needed to run `go test -race`. Setup details are in
`docs/verify.md` section 0.

## Agent quick start

```powershell
cd agent
copy config.example.json config.json
# edit config.json and set a non-empty "token"
go run ./cmd/spotdash
```

The agent refuses to start if `config.json` is missing, malformed, or has an
empty token. Open `http://localhost:8765/?token=<your token>` in a browser.

`config.json` holds a shared secret and is excluded by `.gitignore`. Keep it
that way.

## Connecting Spotify

`spotify` has two modes, set with the required `mode` key (no default).

**Mock**, no account or network needed:

```json
"spotify": {
  "enabled": true,
  "interval_ms": 1000,
  "mode": "mock",
  "track": "Track name",
  "artist": "Artist name",
  "album": "Album name",
  "duration_ms": 342000,
  "layout": "fill"
}
```

**API**, the real thing. Setup:

1. Create an app at <https://developer.spotify.com/dashboard>, tick **Web API**.
2. Add redirect URI `http://127.0.0.1:8765/spotify/callback` exactly (match
   your `listen` port if it differs).
3. New apps start in Development Mode with a login allowlist. Add your own
   account under **Users Management** before connecting.
4. Copy the **Client ID**. No client secret needed, this uses PKCE.

```json
"spotify": {
  "enabled": true,
  "interval_ms": 5000,
  "mode": "api",
  "client_id": "your client id",
  "redirect_uri": "http://127.0.0.1:8765/spotify/callback",
  "state_file": "spotify_state.json",
  "layout": "fill"
}
```

`state_file` holds the refresh token, written by the agent. Keep it next to
`config.json` and out of git, same as the token.

With the agent running, open `http://<agent host>:<port>/spotify/connect` in a
browser (token as `?token=`, or already set as a session cookie from the
panel). Completing consent lands on a page that says **Connected**.

Scope is read-only, no play or pause from the panel. If the connection drops,
`/health` and the status face show why and link back to `/spotify/connect`.

## Web UI without the agent

`agent/web/dev.html` renders the UI inside a 480x480 circle in a desktop
browser, with a toolbar to switch faces and inject fake state. No agent and no
build step required. Open the file directly.

## Shell quick start

Create and launch the 480x480 emulator, which matches the real device:

```powershell
cd shell	ools
.vd.ps1
```

Build and install. A clean checkout needs only `ANDROID_HOME` set; the gitignored
`local.properties` is not required.

```powershell
cd shell
.\gradlew assembleDebug
adb install -r appuild\outputspk\debugpp-debug.apk
adb shell am start -n dev.spotdash.shell/.PanelActivity
```

Press and hold the display for three seconds to open settings, then enter the
agent URL and token. From the emulator the host is `http://10.0.2.2:8765`. That
gesture is the only UI the shell has beyond the WebView, because the device has
no other input.

On the real device, set the shell as the default launcher so the panel survives
a reboot without anyone touching it.

## Security posture

Phase 1 uses a shared bearer token over plain HTTP on a trusted LAN. There is no
TLS. This is a deliberate, documented tradeoff. See `docs/architecture.md`.

## Documentation

- `docs/architecture.md` for component boundaries, data flow, and the source contract.
- `docs/verify.md` for the commands used to verify each piece of work, and their output.
- `docs/device.md` for bringing up the real Echo Spot.
