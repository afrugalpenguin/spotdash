# Verification log

Every working-order item ends with commands that were actually run and the
output they actually produced. Nothing in this file is a description of what
would happen. If a section still says `Not yet verified`, that item is not done.

Commands are given for PowerShell on Windows unless stated otherwise.

## 0. Development prerequisites

Status: verified on 2026-09-14.

| Tool                | Why                                                    |
| ------------------- | ------------------------------------------------------ |
| Go 1.25 or later    | The agent.                                             |
| mingw-w64 gcc       | cgo, which the NVML binding needs, and the race detector. |
| JDK 17 or later     | The shell.                                             |
| Android SDK, API 30 | The shell and its emulator.                            |

### The C toolchain

Go decides the default value of `CGO_ENABLED` by looking for a C compiler on
PATH. With none found it silently defaults to 0, and both cgo builds and
`go test -race` then fail. This is the failure that section 2 originally
recorded as an unfixable limitation.

On this machine MSYS2 was already present at `C:\msys64` with a working
mingw-w64 gcc, just not on PATH. Adding it to the user PATH is the whole fix:

```powershell
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$raw = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$key.SetValue('Path', $raw.TrimEnd(';') + ';C:\msys64\mingw64\bin', [Microsoft.Win32.RegistryValueKind]::ExpandString)
$key.Close()
```

The registry value is written directly rather than through
`[Environment]::SetEnvironmentVariable`, which rewrites the value as a plain
string. This PATH is a `REG_EXPAND_SZ` containing `%USERPROFILE%` entries, and
flattening it would leave those entries as literal unexpanded text.

If MSYS2 is not installed, `winget install MSYS2.MSYS2` followed by
`C:\msys64\usr\bin\pacman -S mingw-w64-x86_64-gcc` produces the same result.

Verification, in a shell started after the PATH change:

```
$ gcc --version
gcc.exe (Rev8, Built by MSYS2 project) 15.2.0

$ go env CGO_ENABLED
1

$ go env CC
gcc
```

`CGO_ENABLED` is left to autodetection rather than pinned in `go env`. Pinning
it to 1 would make the build fail on a machine without a compiler instead of
falling back, which is the wrong trade for a project that only needs cgo for one
optional source.

A cgo program compiles, links, and runs:

```
$ go build -o cgocheck.exe .
$ ./cgocheck.exe
cgo linked and callable, twice(21) = 42
```

And the agent suite passes under the race detector:

```
$ go test -race ./...
ok      github.com/afrugalpenguin/spotdash/agent/internal/config   1.310s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging  1.154s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server   1.363s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state    1.129s
```

Open question deferred to item 6: a cgo-linked binary may pick up a runtime
dependency on the mingw DLLs, which would break the single static binary goal on
a machine without MSYS2. Item 6 checks the produced binary with `ldd` or an
equivalent and adds `-extldflags "-static"` if needed.

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

Status: verified on 2026-09-14.

### Unit tests

```
$ cd agent
$ go vet ./...
$ gofmt -l .
$ go test ./...
ok      github.com/afrugalpenguin/spotdash/agent/internal/config   0.246s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging  0.208s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server   0.282s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state    0.181s
```

`go vet` and `gofmt -l` both printed nothing, which is the passing result.

The suite also passes under the race detector. See section 0 for how that was
enabled.

```
$ go test -race ./...
?       github.com/afrugalpenguin/spotdash/agent/cmd/spotdash      [no test files]
ok      github.com/afrugalpenguin/spotdash/agent/internal/config   1.310s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging  1.154s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server   1.363s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state    1.129s
```

### Build

```
$ go build -ldflags "-X main.version=0.1.0-item2" -o spotdash.exe ./cmd/spotdash
$ ./spotdash.exe -version
0.1.0-item2
```

### Fail-closed startup

Each case below must exit non-zero and name the offending key. `$T` is an empty
temporary directory.

```
$ ./spotdash.exe -config "$T/config.json"
spotdash: config file not found at C:/.../config.json: create it from config.example.json
exit=1

$ echo '{"token":"","sources":{}}' > "$T/config.json"
$ ./spotdash.exe -config "$T/config.json"
spotdash: in C:/.../config.json: "token" is required and must not be empty: the agent will not serve data without a shared secret
exit=1

$ echo '{"token":"abc",}' > "$T/config.json"
$ ./spotdash.exe -config "$T/config.json"
spotdash: parsing C:/.../config.json: invalid character '}' looking for beginning of object key string
exit=1

$ echo '{"token":"abc","listn":"0.0.0.0:8765"}' > "$T/config.json"
$ ./spotdash.exe -config "$T/config.json"
spotdash: parsing C:/.../config.json: json: unknown field "listn"
exit=1

$ echo '{"token":"abc","sources":{"clock":{"enabled":true}}}' > "$T/config.json"
$ ./spotdash.exe -config "$T/config.json"
spotdash: in C:/.../config.json: source "clock" is enabled but its "interval_ms" is 0, want a positive number of milliseconds
exit=1
```

The fourth case matters as much as the empty token: a mistyped key is silently
ignored by most JSON loaders, which is how a config ends up not meaning what it
looks like it means.

### /health and auth against a running agent

Started with `listen` `127.0.0.1:8765`, token `verify-token-item2`, `clock`
enabled and `telemetry` disabled.

```
$ curl -s -i http://127.0.0.1:8765/health
HTTP/1.1 200 OK
Cache-Control: no-store
Content-Type: application/json; charset=utf-8
Content-Length: 164

{"version":"0.1.0-item2","uptime_seconds":5.4692583,"sources":{"clock":{"status":"degraded","last_error":"awaiting first poll"},"telemetry":{"status":"disabled"}}}
```

`clock` reads `degraded` because the registry does not exist yet, so nothing has
polled it. That flips to `ok` in item 3. `telemetry` is `disabled` because config
says so.

```
$ curl -s -i http://127.0.0.1:8765/
HTTP/1.1 401 Unauthorized
Www-Authenticate: Bearer realm="spotdash"

$ curl -H 'Authorization: Bearer wrong' http://127.0.0.1:8765/
status=401

$ curl -H 'Authorization: Bearer verify-token-item2' http://127.0.0.1:8765/
status=404

$ curl http://127.0.0.1:8765/secret-route
status=401
```

The 404 is correct for this item: the token was accepted and routing then found
nothing, because the UI and the WebSocket arrive in items 4 and 5. The last case
is the one worth keeping: an unknown path returns 401 rather than 404, so an
unauthenticated caller cannot map which routes exist.

### Logging

```
$ cat spotdash.log
time=2026-09-14T08:55:01.473+01:00 level=INFO msg=starting version=0.1.0-item2 config=C:/.../config.json log_file=C:\...\spotdash.log listen=127.0.0.1:8765 log_level=debug
time=2026-09-14T08:55:01.474+01:00 level=DEBUG msg="registered source" source=clock enabled=true
time=2026-09-14T08:55:01.474+01:00 level=DEBUG msg="registered source" source=telemetry enabled=false
time=2026-09-14T08:55:01.474+01:00 level=INFO msg=listening addr=127.0.0.1:8765
```

Records go to stderr and to the rotating file at the same time.

## 3. Source registry and the clock source

Status: verified on 2026-09-14.

### Unit tests

```
$ go vet ./... && gofmt -l .
$ go test ./...
?       github.com/afrugalpenguin/spotdash/agent/cmd/spotdash          [no test files]
ok      github.com/afrugalpenguin/spotdash/agent/internal/config        0.230s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging       0.212s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server        0.282s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources       0.430s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources/clock 0.218s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state         0.184s

$ go test -race -count=1 ./...
?       github.com/afrugalpenguin/spotdash/agent/cmd/spotdash          [no test files]
ok      github.com/afrugalpenguin/spotdash/agent/internal/config        1.317s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging       1.282s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server        1.375s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources       1.470s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources/clock 1.159s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state         1.129s
```

The runner tests cover the parts that are hard to eyeball: a panic in `Poll`
recorded as an error with the source recovering on a later poll, one source
panicking while another keeps reporting `ok`, `Poll` receiving a context with a
deadline, the backoff curve growing and capping without overflowing, and a
failing source not spinning. The clock tests cover the sleep window on both
sides of every boundary, including the midnight crossing.

### Registry fail-closed paths

```
$ ./spotdash.exe -config bad.json   # {"sources":{"spotify":{"enabled":true,...}}}
spotdash: config names an unknown source "spotify"
exit=1

$ ./spotdash.exe -config bad.json   # {"sources":{"clok":{"enabled":false,...}}}
spotdash: config names an unknown source "clok"
exit=1

$ ./spotdash.exe -config bad.json   # clock with "sleep_start":"25:00"
spotdash: source "clock": "sleep_start" "25:00": hour 25 is out of range, want 0 to 23
exit=1
```

The middle case is the one worth keeping. The source is disabled, so nothing
would have run either way, but `clok` is how a working clock face silently stops
appearing.

### Clock source running

Config: `clock` enabled at `interval_ms` 1000.

```
$ curl -s http://127.0.0.1:8765/health
{"version":"0.1.0-item3","uptime_seconds":5.2572229,"sources":{"clock":{"status":"ok","last_update":"2026-09-14T08:15:10Z"}}}

$ sleep 3 && curl -s http://127.0.0.1:8765/health
{"version":"0.1.0-item3","uptime_seconds":8.343657199999999,"sources":{"clock":{"status":"ok","last_update":"2026-09-14T08:15:13Z"}}}
```

`clock` reports `ok` and `last_update` advances from `:10` to `:13`, so it is
being polled rather than reporting a single startup reading.

Per-poll debug logging, one record per second as configured:

```
$ grep -c "source poll ok" spotdash.log
15
$ grep "source poll ok" spotdash.log | tail -3
time=2026-09-14T09:15:17.622+01:00 level=DEBUG msg="source poll ok" source=clock duration=0s
time=2026-09-14T09:15:18.623+01:00 level=DEBUG msg="source poll ok" source=clock duration=0s
time=2026-09-14T09:15:19.623+01:00 level=DEBUG msg="source poll ok" source=clock duration=0s
```

### Disabled source

Same config with `"enabled": false`:

```
$ curl -s http://127.0.0.1:8766/health
{"version":"0.1.0-item3","uptime_seconds":5.7123188,"sources":{"clock":{"status":"disabled"}}}

$ grep "sources started" spotdash.log
time=2026-09-14T09:15:33.032+01:00 level=INFO msg="sources started" count=0

$ grep -c "source poll ok" spotdash.log
0
```

Reported as `disabled`, no goroutine started, and no polls. A disabled source
costs nothing at runtime rather than being polled and discarded.

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

Status: verified on 2026-09-14. Brought forward ahead of item 4 so the layout
could be reviewed before live data was wired underneath it. The one acceptance
point that needs the socket, the clock face updating live, is verified in item 4.

### Tests

```
$ go test ./internal/server/
ok      github.com/afrugalpenguin/spotdash/agent/internal/server        0.281s

$ cd web && node --test app.test.js
tests 16
pass 16
fail 0
```

The Go tests were written first, as everywhere else. The JavaScript tests were
not: they were written after the code, so they were never seen failing for the
right reason. To establish they test something, two were checked by mutation and
then restored:

```
$ # readToken no longer strips the token from the URL
FAIL: readToken takes the token out of the URL
pass 15   fail 1

$ # uptime of zero reports "0s" instead of "unknown"
FAIL: uptime of zero reads as unknown rather than a restart
pass 15   fail 1

$ # both mutations reverted
pass 16   fail 0
```

### Serving the embedded UI

```
$ curl -o /dev/null -w '%{http_code}' http://127.0.0.1:8765/
401
$ curl -o /dev/null -w '%{http_code}' http://127.0.0.1:8765/app.js
401

$ curl -H 'Authorization: Bearer <token>' http://127.0.0.1:8765/
200 text/html; charset=utf-8
$ curl -H 'Authorization: Bearer <token>' http://127.0.0.1:8765/app.js
200 text/javascript; charset=utf-8
$ curl -H 'Authorization: Bearer <token>' http://127.0.0.1:8765/style.css
200 text/css; charset=utf-8
$ curl -H 'Authorization: Bearer <token>' http://127.0.0.1:8765/faces/clock.js
200 text/javascript; charset=utf-8
```

The content types are set from an explicit table rather than by
`mime.TypeByExtension`. On Windows that consults the registry, where `.js` is
routinely registered as `text/plain`, and a module served as `text/plain` is
refused by the browser. The panel then renders blank with nothing to indicate
why.

### How a browser actually loads the panel

A browser cannot set a header when navigating, and the stylesheet and modules
the page then requests carry neither a header nor a query string. Verified with
a cookie jar, which is exactly what a browser does:

```
$ curl -i -c jar.txt "http://127.0.0.1:8765/?token=<token>"
HTTP/1.1 200 OK
Cache-Control: no-store
Content-Type: text/html; charset=utf-8
Set-Cookie: spotdash_session=<token>; Path=/; HttpOnly; SameSite=Strict

$ curl -b jar.txt http://127.0.0.1:8765/app.js
200
$ curl -b jar.txt http://127.0.0.1:8765/style.css
200
$ curl -b jar.txt http://127.0.0.1:8765/faces/clock.js
200
```

No `Expires` and no `Max-Age`, so the cookie lives for the browser session and
is never written to disk. A forged cookie value is rejected, and a request that
authenticated by header is given no cookie at all. Both are covered by tests.

### The faces against the real agent

Screenshots taken with headless Chrome at exactly 480x480, against the running
agent with `clock` enabled:

```
$ chrome --headless=new --window-size=480,480 --virtual-time-budget=4000 \
    --screenshot=agent-status.png "http://127.0.0.1:8765/?token=<token>&face=status"
```

The status face rendered `clock  ok  1s`, `up 15s`, `link connecting`, with a
full teal rim for the single healthy source. Those values came from the running
agent through `/health`, not from fixtures.

The clock face rendered `--:--` and `waiting for the agent`, which is correct
for this item: the clock reading arrives over the socket, and the socket is
item 4.

### dev.html with no agent running

```
$ cd agent/web && python -m http.server 8099
$ curl -o /dev/null -w '%{http_code}' http://127.0.0.1:8099/dev.html
200
```

Every face was rendered against every state and reviewed as a screenshot:
`running`, `sleeping`, `one source degraded`, `source disabled`,
`agent unreachable`, and `nothing reported`. Confirmed by eye:

- The sleep state renders true black with one dim dot and nothing else.
- The status face is fully readable while disconnected, with stale ages, the
  last error, and a red connection dot.
- The empty state reads `no sources reported yet` rather than rendering nothing.
- The segmented rim shows one arc per source, amber for the degraded one.

`dev.html` takes `?face=` and `?state=` so a particular combination can be
reopened directly, which is how the screenshots above were captured.

### Two review findings, both fixed

Reviewing the screenshots rather than assuming they were right caught two
things. The status face showed `up 0s` when the agent was unreachable, which
reads as a restart that did not happen; zero now renders as `unknown`. And the
face carried a `sources` heading above a list that was self-evidently a list of
sources, which has been removed.

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
