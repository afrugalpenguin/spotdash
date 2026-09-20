# Verification log

Every item below ends with commands that were actually run and the output they actually produced, not a description of what would happen. PowerShell unless stated otherwise.

## 0. Development prerequisites

| Tool                | Why                                                    |
| ------------------- | ------------------------------------------------------ |
| Go 1.25+            | The agent.                                             |
| mingw-w64 gcc       | cgo (NVML binding) and the race detector.              |
| JDK 17+             | The shell.                                             |
| Android SDK, API 30 | The shell and its emulator.                            |

### C toolchain

Go picks `CGO_ENABLED` by looking for a C compiler on PATH; none found -> silently 0, and both cgo builds and `go test -race` fail. Fix: add `C:\msys64\mingw64\bin` to user PATH (MSYS2 was already installed, just not on PATH):

```powershell
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$raw = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$key.SetValue('Path', $raw.TrimEnd(';') + ';C:\msys64\mingw64\bin', [Microsoft.Win32.RegistryValueKind]::ExpandString)
$key.Close()
```

Written directly to the registry, not via `[Environment]::SetEnvironmentVariable`, because that flattens the existing `REG_EXPAND_SZ` (with `%USERPROFILE%` entries) into a literal string.

No MSYS2: `winget install MSYS2.MSYS2` then `C:\msys64\usr\bin\pacman -S mingw-w64-x86_64-gcc`.

```
$ gcc --version          # gcc.exe (Rev8, Built by MSYS2 project) 15.2.0
$ go env CGO_ENABLED      # 1
$ go env CC               # gcc
$ go build -o cgocheck.exe . && ./cgocheck.exe
cgo linked and callable, twice(21) = 42
$ go test -race ./...
ok      .../agent/internal/config   1.310s
ok      .../agent/internal/logging  1.154s
ok      .../agent/internal/server   1.363s
ok      .../agent/internal/state    1.129s
```

`CGO_ENABLED` left to autodetection (not pinned) so a machine without a compiler still builds, since cgo is only needed for one optional source.

Deferred to item 6: whether a cgo-linked binary picks up a mingw DLL dependency, breaking the static-binary goal.

## 1. Repository scaffold and docs

```
$ git ls-files
.gitattributes .gitignore README.md docs/architecture.md docs/verify.md

$ echo '{}' > agent/config.json && git check-ignore -v agent/config.json
.gitignore:3:agent/config.json  agent/config.json
```

House rule: no em/en dashes anywhere in the repo.

```
$ grep -rnP '[\x{2013}\x{2014}]' . --exclude-dir=.git
$ echo "exit=$?"   # exit=1, no matches
```

Toolchain on the dev machine: Go 1.26, OpenJDK 25 (Temurin), RTX 4070 Ti SUPER / driver 616.64 (needed for item 6's NVML path).

## 2. Config loading and /health

```
$ cd agent
$ go vet ./... && gofmt -l .        # both clean
$ go test ./...                     # all packages ok
$ go test -race ./...               # all packages ok
$ go build -ldflags "-X main.version=0.1.0-item2" -o spotdash.exe ./cmd/spotdash
$ ./spotdash.exe -version           # 0.1.0-item2
```

Fail-closed startup, each case exits non-zero naming the offending key:

```
$ ./spotdash.exe -config "$T/config.json"                          # missing file
spotdash: config file not found at ...: create it from config.example.json

$ echo '{"token":"","sources":{}}' > "$T/config.json"; ./spotdash.exe -config "$T/config.json"
spotdash: in ...: "token" is required and must not be empty

$ echo '{"token":"abc",}' > "$T/config.json"; ./spotdash.exe -config "$T/config.json"
spotdash: parsing ...: invalid character '}' looking for beginning of object key string

$ echo '{"token":"abc","listn":"0.0.0.0:8765"}' > "$T/config.json"; ./spotdash.exe -config "$T/config.json"
spotdash: parsing ...: json: unknown field "listn"

$ echo '{"token":"abc","sources":{"clock":{"enabled":true}}}' > "$T/config.json"; ./spotdash.exe -config "$T/config.json"
spotdash: in ...: source "clock" is enabled but its "interval_ms" is 0, want a positive number
```

The unknown-field case matters as much as the empty token - a mistyped key silently ignored is how a config stops meaning what it looks like.

`/health` and auth, agent on `127.0.0.1:8765`, `clock` enabled, `telemetry` disabled:

```
$ curl -s -i http://127.0.0.1:8765/health
{"version":"0.1.0-item2","uptime_seconds":5.47,"sources":{"clock":{"status":"degraded","last_error":"awaiting first poll"},"telemetry":{"status":"disabled"}}}

$ curl -s -i http://127.0.0.1:8765/                              # 401, no auth
$ curl -H 'Authorization: Bearer wrong' .../               # 401
$ curl -H 'Authorization: Bearer verify-token-item2' .../  # 404 (routing not wired yet, expected here)
$ curl http://127.0.0.1:8765/secret-route                        # 401, not 404 - unauthenticated caller can't map routes
```

Logging goes to stderr and the rotating file at once.

## 3. Source registry and the clock source

```
$ go vet ./... && gofmt -l . && go test ./... && go test -race -count=1 ./...
```

All packages pass, including runner tests for: panic in `Poll` recovered and recorded, one source panicking doesn't affect another, `Poll` gets a deadline context, backoff grows and caps without overflow, a failing source doesn't spin. Clock tests cover the sleep window boundaries including midnight.

Registry fail-closed:

```
$ ./spotdash.exe -config bad.json   # unknown source "spotify"
spotdash: config names an unknown source "spotify"

$ ./spotdash.exe -config bad.json   # disabled source named "clok"
spotdash: config names an unknown source "clok"    # matters: this is how a working clock face silently stops appearing

$ ./spotdash.exe -config bad.json   # sleep_start "25:00"
spotdash: source "clock": "sleep_start" "25:00": hour 25 is out of range
```

Clock running at `interval_ms` 1000: `/health` shows `status: ok`, `last_update` advancing second to second; debug log shows one "source poll ok" per second. Disabled: `status: disabled`, zero goroutines, zero polls.

## 4. WebSocket broadcast

```
$ go test -race -count=1 ./...
```

Store tests cover 500 updates against a subscriber that never reads, completing without blocking, and that subscriber getting dropped rather than silently skipping messages.

`wsprobe` (dials like the panel does):

```
$ wsprobe -count 1                          # no token -> 401 at handshake
$ wsprobe -token wrong -count 1             # wrong token -> 401
$ wsprobe -token <token> -count 4
connected, negotiated subprotocol "spotdash.v1"
snapshot {"source":"clock",...}
update   {"source":"clock",...}  x3, one per second
```

Clock face rendered live via headless Chrome screenshot: `11:01`, `Mon 14 Sep`, seconds arc ~3/4 filled, teal connection dot.

**Found via a slow test, not inspection**: every socket test took exactly 5.00s (a timeout, not real work), because the panel-only-listens design meant the handler never read the connection, so a client close was never acknowledged until timeout - same defect means a vanished device is never noticed either. Fixed with `CloseRead` (drains frames, cancels handler context on peer-gone) plus a 30s keepalive ping. Same tests after: 0.00s each, 0.725s total for the package.

**Measurement artifact, checked not assumed**: a screenshot showed `clock ok 7s` for a 1s-interval source. Cause: `--virtual-time-budget` fast-forwards `Date.now()` while socket messages arrive in real time. A shorter budget reads `1s` correctly. Surfaced a real hardware risk though - age is browser clock minus agent timestamp, and the Echo Spot has no battery-backed RTC. Filed as issue 12.

## 5. Static UI, status and clock faces, dev.html

Brought forward ahead of item 4 to review layout before live data. The one point needing the socket (clock face updating live) is covered in item 4.

```
$ go test ./internal/server/                          # ok
$ cd web && node --test app.test.js                   # 16/16 pass
```

JS tests were written after the code (unlike everywhere else), so verified by mutation: breaking `readToken`'s URL-stripping, and the "uptime 0 renders as 0s instead of unknown" case, each correctly failed 1 test; both reverted to 16/16.

Serving: `/`, `/app.js` etc. all 401 with no auth, 200 with a bearer token, correct content-type per file (`text/javascript`, `text/css` - not left to `mime.TypeByExtension`, which on Windows reads the registry and often has `.js` misregistered as `text/plain`, which the browser then refuses to run).

Browser auth flow verified with a cookie jar (can't set headers on navigation): `?token=` sets `spotdash_session` (HttpOnly, SameSite=Strict, no Expires/Max-Age), subsequent asset requests succeed off the cookie alone.

Faces rendered at 480x480 against the running agent (clock enabled): status face showed `clock ok 1s`, `up 15s`, full teal rim; clock face showed `--:--`/`waiting for the agent` (correct here, socket lands in item 4).

`dev.html` (served standalone, no agent) takes `?face=`/`?state=` and was reviewed by eye across every state - running, sleeping, one degraded, one disabled, agent unreachable, nothing reported. Two findings fixed: `up 0s` when unreachable read like a restart (now renders `unknown`), and a redundant "sources" heading over an obviously-a-list-of-sources was removed.

## 6. Telemetry source, including the NVML degraded path

```
$ go vet ./... && gofmt -l . && go test -race -count=1 ./...   # all ok
```

GPU reading cross-checked against `nvidia-smi` on the same card, seconds apart - name/VRAM-total/temp/power matched exactly, utilisation/VRAM-used differed as expected (both volatile, sampled seconds apart).

Degraded path exercised through the whole agent (real registry, state store, HTTP server, WebSocket client), only the GPU reader swapped for one that fails like a missing driver:

```
$ go test ./internal/sources/telemetry/ -run 'TestAgentKeeps|TestDegradedReading' -v
--- PASS: TestAgentKeepsRunningWhenNVMLIsUnavailable
--- PASS: TestDegradedReadingStillCarriesTheMachineOverTheSocket
```

Confirms: `/health` shows telemetry `degraded` with the NVML reason, socket still carries real CPU/RAM/disk, `gpu` serialises as `null` (not an empty object the panel would render as zeroes). DLL-rename variant not attempted (needs admin rights, would break GPU monitoring machine-wide); stubbing the reader covers it without going that far.

Two findings that changed the build:

- `go-nvml` can't build on Windows (`dlfcn.h`, POSIX-only, no guarding build tags) -> replaced with a ~100-line direct `nvml.dll` binding via `windows.NewLazySystemDLL` (resolves only from the system directory, so a stray DLL beside the binary can't be substituted).
- That removes cgo from the project entirely: `CGO_ENABLED=0 go build` succeeds, and a scan of the produced binary shows zero mingw runtime references and an `nvml.dll` reference only as a lazy runtime load. Mingw is still needed for `go test -race`, not for the shipped artefact.
- `go vet` caught a real bug in the first NVML binding: converting `nvmlErrorString`'s return pointer into Go-owned memory. Return codes are stable API, so they're mapped locally instead.

## 7. Telemetry face

Tests written before the code this time.

```
$ cd web && node --test app.test.js telemetry.test.js    # 28/28 pass
$ go vet ./... && gofmt -l . && go test -race -count=1 ./...
```

Threshold tests sit on the boundaries: 80 ok / 80.1 amber, 95 amber / 95.1 red, 83C ok / 83.1C red. Missing-reading is tested as a distinct third state, neither calm nor alarming.

Rendered under load (CPU 91, RAM 84, GPU 99, VRAM 97, 86C): CPU/RAM amber, GPU/VRAM red, centre temp red with wattage beneath - every threshold landed on the right colour. Idle against the real card: all four quadrants teal with live values. Degraded payload (`gpu: null`): CPU/RAM live teal, GPU/VRAM render dimmed `n/a`, reason stated as `no gpu reading` in amber (stating the reason matters - two empty gauges alone reads as "GPU idle", which is wrong).

Adding the face needed one new function in the shared rim module.

## 8. Tray integration and graceful shutdown

```
$ go test -race -count=1 ./internal/app/ -v
--- PASS (all 10 cases)
```

`TestReloadWithAnInvalidConfigKeepsServing` matters most: a mistyped key in a running agent has to be reported without taking the panel dark - asserts both the error message and that the previous token still works. `TestOpenURLTargetsLoopbackAndCarriesTheToken` covers two ways the tray's "Open UI" item could be useless: shipped config listens on `0.0.0.0` (a browser can't open that), and a URL missing the token 401s.

Graceful shutdown needed a real console control event, not `kill -TERM` (MSYS `kill` calls `TerminateProcess` on Windows, which never reaches Go's signal handler - looks like a pass, proves nothing). Harness starts the agent as a child sharing its console, raises `CTRL_C_EVENT`, reads the exit code. Its own ignore-handler has to be set *after* starting the child (the flag is inherited - set first, the agent ignores the event too, a false pass this harness produced before the ordering was fixed).

```
serving before the event: True   exited: yes   exit code: 0   port 8765: released
```

Log shows shutdown requested, both sources stopped, stopped cleanly. Repeated with the tray enabled (exercises the systray event loop and the path where a signal takes the tray down too) - same clean result.

`-no-tray` runs as a plain console process, which is what the harness above needs and what to use running from a terminal generally.

## 9. Shell in the 480x480 emulator

```
$ cd shell\tools && .\avd.ps1 -CreateOnly
spotdash_480 configured at 480x480, API 30, 1024 MB
$ adb shell wm size        # 480x480
$ adb shell wm density     # 240
$ adb shell getprop ro.build.version.sdk   # 30
```

Emulated display also set circular so off-circle content disappears here too. Two script gotchas found the hard way: `avdmanager`'s stdin prompt rejects UTF-16-with-BOM from a PowerShell pipe (routed through `cmd` instead), and `local.properties` needs escaped/forward-slash paths on Windows (a raw path fails with an error naming neither file nor setting).

```
$ .\gradlew assembleDebug   # BUILD SUCCESSFUL
$ adb install -r app\build\outputs\apk\debug\app-debug.apk   # Success
```

Three defects only the emulator caught:

- Launcher crashed on start - `goFullscreen()` ran before `setContentView`, decor view didn't exist yet, insets controller was null. Matters because the shell is the HOME launcher, so this crash = no launcher at all.
- Panel rendered at 1.5x and fell off the glass - CSS pixels are density-independent, this display is 240dpi, so the fixed 480 CSS px layout became 720 physical px. Fixed by computing scale from real display width instead of pinning `initial-scale=1`.
- A stray amber line down the centre - a tap leaves the zone button focused, WebView draws its own focus ring, the circular clip hides 3 of 4 edges and leaves the inner vertical edge visible. Suppressed for touch focus, kept for keyboard focus (real navigation, not a touch artefact).

Panel ran live in the shell: clock face at 480x480 with teal connection dot, tap advanced to telemetry showing live host values. Settings screen opened on a genuine 3s press and saved successfully.

Fallback and recovery: agent stopped, shell noticed immediately, showed the real connection error after the 30s threshold, retried on schedule, and recovered with no intervention once the agent came back.

One false alarm, checked rather than assumed: a screenshot showed an amber dot and frozen gauges mid-verification. Not a reconnect failure - the agent had just been restarted (log level change) and the console message was the old app instance timing out against a now-closed port. Agent log confirmed one socket open and holding.

## 10. End to end

Fresh clone, nothing carried over from prior state.

```
$ git clone <repo> spotdash-clean-checkout && cd spotdash-clean-checkout
$ ls agent/     # cmd config.example.json go.mod go.sum internal run.ps1 web
# no config.json, no binary
```

Refuses to start before configured:

```
$ go build -o spotdash.exe ./cmd/spotdash && ./spotdash.exe
spotdash: config file not found: ... Copy config.example.json to config.json, or pass -config

$ cp config.example.json config.json   # then blank the token
$ ./spotdash.exe
spotdash: in ...: "token" is required and must not be empty
```

Example config ships with a placeholder token, and the agent refuses to start while the token is still that placeholder, so a fresh clone can't accidentally serve with a public secret.

```
$ go vet ./... && gofmt -l .              # both clean
$ go test -race -count=1 ./...            # all packages ok
$ cd web && node --test app.test.js telemetry.test.js   # 28/28 pass
```

Serving with a real token: `/health` shows both sources `ok`; `/` 401 without token, 200 with. `wsprobe` gets snapshot + live updates. All three faces rendered correctly at 480x480 from the clean build.

Shell from the same clone needs only `ANDROID_HOME` set (`local.properties` is gitignored and optional - present-and-wrong fails the build with an error naming neither file nor setting):

```
$ .\gradlew assembleDebug   # BUILD SUCCESSFUL
$ ls app\build\outputs\apk\debug\app-debug.apk   # 9711395 bytes
```

APK has no native libraries, so this ARM-targeting build never needed touching for the x86_64 emulator either.

One blemish fixed: the not-configured message listed the same resolved path twice (both candidates pointed at the same place) - read like a bug in the message rather than a missing file. Deduplicated.

### What phase 1 doesn't cover

Everything needing the hardware - see `docs/device.md`. One known defect waiting there: issue 12, source ages are device-clock-minus-agent-timestamp and the Echo Spot has no battery-backed RTC.
