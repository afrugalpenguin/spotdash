# spotdash

[![Go version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](agent/go.mod)
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

## Hardware

Built for a 2017 Echo Spot (`rook`) on LineageOS 18.1, 480x480 circular screen, about 2 GB RAM (`MemTotal` 1958192 kB, and the ROM sets `ro.config.low_ram=true`). You don't need one to hack on this though: the UI runs in a browser and the shell runs in a 480x480 emulator.

## Use it

You need an Echo Spot (2017, `rook`), a Windows PC on the same network, a micro USB cable and Android [platform-tools](https://developer.android.com/tools/releases/platform-tools) (`adb`) on your `PATH`. Nothing else has to be installed.

### 1. Root and flash the Spot

A stock Spot cannot run the shell as its home app. [`docs/rooting.md`](docs/rooting.md) walks from a stock device to LineageOS 18.1. [`docs/device.md`](docs/device.md) covers bringing the flashed device up and has the troubleshooting section.

### 2. Download and run the agent

Download the zip from the [releases page](https://github.com/afrugalpenguin/spotdash/releases) and unzip it. To check the download first, see "Verify your download" below. Run `spotdash.exe`: it sits in the tray and opens the panel in your browser.

The exe is not code signed: this is a one-person desk toy and a signing certificate is not worth the cost. SmartScreen will flag it as an unrecognised publisher on first run ("More info", then "Run anyway"); that's expected.

With no config the agent makes one: `%APPDATA%\spotdash\config.json`, with a random token, the same defaults as `config.example.json`, and the panel opens in your browser. It looks for a config in this order and uses the first it finds: `-config <path>`, `config.json` next to the executable, `config.json` in the working directory, then that per-user file. The log and the Spotify state file live next to whichever it used. `spotdash.exe -config-path` prints which one that is without creating anything. An existing config is never replaced, even a broken one: fix it, or point `-config` somewhere else.

The example also has `calendar` and `spotify` blocks, switched off with placeholder values. Fill one in and set `"enabled": true` to use it (see "Spotify and calendar" below). The agent reads `config.json` when it starts, and again on **Reload config** in the tray or when the settings page is saved. Any other edit needs one of those, or a restart.

The panel is at `http://localhost:8765/?token=<your token>`. The agent won't start with an empty or placeholder token in `config.json` (which is gitignored, keep it that way).

### 3. Set up the Echo Spot

With the agent running, the Spot flashed and connected by USB with USB debugging on, one command from the unzipped folder does the rest:

```powershell
powershell -ExecutionPolicy Bypass -File tools\provision.ps1
```

It installs the shell APK from `shell\` in the zip, hands the shell the agent address and token, makes the shell the home app, sets the timezone from this PC, sets brightness, adds a Windows Firewall rule for the agent port that only the Spot can use, and checks that the panel loads. Each step prints `[PASS]`, `[SKIP]` or `[FAIL]` and the run stops at the first failure with what to do next. Running it again is safe. It needs adb on `PATH`, a Spot that allows `adb root`, and an elevated PowerShell for the firewall step (add `-SkipFirewall` to leave that step out). The token is never printed. A transcript goes to `provision-<time>.log` next to the script. `Get-Help .\tools\provision.ps1 -Full` lists the options. If a step fails, see the troubleshooting section of [`docs/device.md`](docs/device.md).

The Wi-Fi button on the shell's setup screen (long-press the display for 3 seconds) opens Android's own Wi-Fi settings, for a device that moves network. That's the only UI the shell has: there is no other input on the real device.

### 4. Start it automatically (optional)

Tick **Start with Windows** in the tray menu. That adds one value to your own login list (no admin rights), and unticking removes it. It won't let you tick it for a copy running from a temp folder, since that won't exist at next login. To remove it by hand:

```bat
reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v spotdash /f
```

There is no console, so everything goes to `spotdash.log` next to `config.json`, including why it failed to start. **View log** in the tray menu opens it. A second copy exits quietly with a line in that log. Relative paths in `config.json` (such as Spotify's `state_file`) mean next to `config.json`, wherever it was started from.

Prefer a scheduled task (restart on failure, a 15 second delay after logon)? `.\tools\autostart.ps1 -Install` registers one instead (`-Status` and `-Uninstall` do what you'd expect). It is not a service: a service has no desktop session to put a tray on. Use one or the other. Running both is safe, and the second copy just exits.

### 5. Spotify and calendar (optional)

Both sources have a `mock` mode for testing and a real mode:

- Spotify: `mode: "api"`, needs a Spotify app (Client ID only, PKCE, no
  secret) and a redirect URI of `http://127.0.0.1:8765/spotify/callback`.
  Then visit `/spotify/connect` to authorise. There is no shared app to use:
  Spotify limits an app like this to five users, so everyone creates their
  own. See [`docs/spotify-setup.md`](docs/spotify-setup.md) for the steps.
- Calendar: `mode: "ics"`, just a published ICS feed URL (Outlook: Share
  calendar > Publish). Keep the link private, anyone with it can read your
  calendar.

Full config keys and setup steps are in `docs/architecture.md`.

### Verify your download

Every release has a `SHA256SUMS` file and a build attestation for the zip and the APK. In a folder with the downloads, check a file against its line in `SHA256SUMS` (Windows, then Git Bash):

```
certutil -hashfile spotdash-<tag>-windows-amd64.zip SHA256
sha256sum -c SHA256SUMS
```

To check that a file was built by this repository's release workflow from the tagged commit:

```
gh attestation verify spotdash-<tag>-windows-amd64.zip --repo afrugalpenguin/spotdash
gh attestation verify spotdash-shell-<tag>.apk --repo afrugalpenguin/spotdash
```

These prove where a file came from, not that it is free of bugs. The exe stays unsigned, so SmartScreen still warns, as described in step 2.

## Build it

| Tool                | For                        |
| -------------------- | --------------------------- |
| Go 1.26+             | the agent                   |
| JDK 17+, Android SDK | the shell                   |
| mingw-w64 gcc        | `go test -race` only        |

### Run the agent from source

```powershell
cd agent
go run ./cmd/spotdash
```

It makes the same per-user config as the release exe on first run (see step 2 above). For the everyday binary with no console window, which is what you want for a tray app that starts at login:

```powershell
cd agent
.\tools\build.ps1
```

### Run the shell in an emulator

Spin up the matching emulator and install the app:

```powershell
cd shell\tools
.\avd.ps1

cd ..
.\gradlew assembleDebug
adb install -r app\build\outputs\apk\debug\app-debug.apk
adb shell am start -n dev.spotdash.shell/.PanelActivity
```

Long-press the display for 3 seconds to enter the agent URL and token (from the emulator that's `http://10.0.2.2:8765`).

Want to work on faces without running the agent at all? Open `agent/web/dev.html` directly, it's got a toolbar for switching faces and faking state.

### Releases

Tag a version (`git tag v0.1.0 && git push --tags`) and CI cross-compiles `spotdash.exe`, builds and signs the shell APK, and attaches both to a GitHub Release. The zip has `spotdash.exe` (no console window, version taken from the tag), `config.example.json`, `tools\autostart.ps1`, `tools\provision.ps1`, the APK under `shell\` and the licence. The release also has the APK on its own as `spotdash-shell-<tag>.apk`, a `SHA256SUMS` file and build attestations. A tag with a hyphen, such as `v0.2.0-rc1`, is marked as a pre-release. Signing is described in [`docs/signing.md`](docs/signing.md).

### Layout

| Path     | What it is                                                   |
| -------- | ------------------------------------------------------------- |
| `agent/` | Go tray app. HTTP server, WebSocket, pluggable data sources.  |
| `shell/` | Kotlin Android app. One Activity, fullscreen WebView.          |
| `docs/`  | Architecture notes and a verification log.                    |

## Security

Shared bearer token over plain HTTP on a trusted LAN, no TLS. That's a
known, accepted tradeoff for a desk toy on a home network. See
`docs/architecture.md` for the reasoning.

## More docs

- [`docs/architecture.md`](docs/architecture.md) - how it's put together, and the full source config reference.
- [`docs/verify.md`](docs/verify.md) - how each piece was tested.
- [`docs/rooting.md`](docs/rooting.md) - getting a stock Echo Spot onto LineageOS.
- [`docs/device.md`](docs/device.md) - bringing up a real Echo Spot.
- [`docs/signing.md`](docs/signing.md) - how the shell APK is signed.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) - house style for commits, pull requests, issues, comments and docs.
