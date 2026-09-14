# Verification log

Every working-order item ends with commands that were actually run and the
output they actually produced. Nothing in this file is a description of what
would happen. If a section still says `Not yet verified`, that item is not done.

Commands are given for PowerShell on Windows unless stated otherwise.

## 1. Repository scaffold and docs

Status: verified on 2026-09-14.

Tracked files after staging the scaffold:

```
$ git ls-files
.gitattributes
.gitignore
README.md
docs/architecture.md
docs/verify.md
```

`config.json` holds the shared token and must be untrackable. Creating one and
asking git whether it is ignored:

```
$ echo '{}' > agent/config.json
$ git check-ignore -v agent/config.json
.gitignore:3:agent/config.json  agent/config.json
```

House rule: no em dashes or en dashes anywhere in the repository. Scanning every
tracked and untracked file for U+2013 and U+2014, which must produce no matches:

```
$ grep -rnP '[\x{2013}\x{2014}]' . --exclude-dir=.git
$ echo "exit=$?"
exit=1
```

Toolchain present on the development machine:

```
$ go version
go version go1.26.0 windows/amd64
$ java -version
openjdk version "25.0.1" 2025-10-21 LTS
OpenJDK Runtime Environment Temurin-25.0.1+8 (build 25.0.1+8-LTS)
$ nvidia-smi --query-gpu=name,driver_version --format=csv
name, driver_version
NVIDIA GeForce RTX 4070 Ti SUPER, 616.64
```

The NVIDIA device matters for item 6: the working NVML path is testable here,
and the degraded path is produced by making NVML unreachable.

## 2. Config loading and /health

Status: not yet verified.

What must be shown:

- The agent exits non-zero with a clear message when `config.json` is absent.
- The agent exits non-zero with a clear message when `token` is empty.
- With a valid config, `GET /health` returns JSON containing uptime, version,
  and a per-source status map.
- `GET /health` succeeds without an `Authorization` header.

```
Not yet verified.
```

## 3. Source registry and the clock source

Status: not yet verified.

What must be shown:

- The registry starts one goroutine for the enabled `clock` source.
- `GET /health` reports `clock` as `ok` with a recent last-update time.
- Disabling `clock` in config makes `/health` report it as `disabled`.

```
Not yet verified.
```

## 4. WebSocket broadcast

Status: not yet verified.

What must be shown:

- A client connecting to `/ws` with a valid token receives a full snapshot.
- Subsequent `clock` updates arrive as separate messages in the documented
  `{source, ts, data}` shape.
- A connection without a token, or with a wrong token, is rejected.

```
Not yet verified.
```

## 5. Static UI, status and clock faces, dev.html

Status: not yet verified.

What must be shown:

- The UI is served from the embedded filesystem and requires the token.
- The `clock` face updates live from the WebSocket.
- The `status` face lists sources, statuses, uptime, and connection state.
- `dev.html` renders all faces with injected fake state and no agent running.

```
Not yet verified.
```

## 6. Telemetry source, including the NVML degraded path

Status: not yet verified.

What must be shown:

- With a working NVIDIA driver, GPU utilisation, VRAM, temperature, and power
  appear in the telemetry payload.
- With NVML unavailable, telemetry still reports CPU, RAM, and disk, the GPU
  fields are null, and the source is `degraded` with the NVML error recorded.

```
Not yet verified.
```

## 7. Telemetry face

Status: not yet verified.

What must be shown:

- Radial gauges for CPU, RAM, GPU, and VRAM, with temperature and power centred.
- Colour thresholds: amber above 80 percent, red above 95 percent, and red above
  83 C for GPU temperature.
- The face renders correctly against the degraded, GPU-null payload.

```
Not yet verified.
```

## 8. Tray integration and graceful shutdown

Status: not yet verified.

What must be shown:

- Tray menu items Open UI, Reload config, and Quit all work.
- Ctrl+C and tray Quit both shut down cleanly, stopping source goroutines and
  closing WebSocket connections without error.

```
Not yet verified.
```

## 9. Shell in the 480x480 emulator

Status: not yet verified.

What must be shown:

- The AVD is created at 480x480, API 30, 1 GB RAM by the provided script.
- The shell installs, launches fullscreen, and loads the agent UI from
  `http://10.0.2.2:8765`.
- The settings screen opens on a three second long press and persists values.
- The fallback screen appears when the agent is stopped and recovers when it
  returns.

```
Not yet verified.
```

## 10. End to end

Status: not yet verified.

A single pass from a clean checkout to the UI running in the emulator, with
every command and its output recorded.

```
Not yet verified.
```
