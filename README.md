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

## Web UI without the agent

`agent/web/dev.html` renders the UI inside a 480x480 circle in a desktop
browser, with a toolbar to switch faces and inject fake state. No agent and no
build step required. Open the file directly.

## Shell quick start

```powershell
cd shell
.\gradlew assembleDebug
```

Then install on an emulator or device. The agent URL and token are entered in
the shell's settings screen, reached by long-pressing the display for three
seconds.

## Security posture

Phase 1 uses a shared bearer token over plain HTTP on a trusted LAN. There is no
TLS. This is a deliberate, documented tradeoff. See `docs/architecture.md`.

## Documentation

- `docs/architecture.md` for component boundaries, data flow, and the source contract.
- `docs/verify.md` for the commands used to verify each piece of work, and their output.
