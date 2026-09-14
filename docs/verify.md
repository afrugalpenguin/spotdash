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

Status: verified on 2026-09-14, after item 5.

### Tests

```
$ go test -race -count=1 ./...
?       github.com/afrugalpenguin/spotdash/agent/cmd/spotdash          [no test files]
ok      github.com/afrugalpenguin/spotdash/agent/internal/config        1.312s
ok      github.com/afrugalpenguin/spotdash/agent/internal/logging       1.291s
ok      github.com/afrugalpenguin/spotdash/agent/internal/server        1.905s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources       1.375s
ok      github.com/afrugalpenguin/spotdash/agent/internal/sources/clock 1.143s
ok      github.com/afrugalpenguin/spotdash/agent/internal/state          1.344s
?       github.com/afrugalpenguin/spotdash/agent/web                   [no test files]
```

The store tests cover the property that matters most: five hundred updates
against a subscriber that never reads complete without blocking, and that
subscriber is then dropped rather than left silently skipping messages.

### A real client outside the browser

`wsprobe` dials the socket exactly as the panel does and prints what arrives.

```
$ wsprobe -count 1
dial failed: failed to WebSocket dial: expected handshake response status code 101 but got 401 (http status 401)

$ wsprobe -token wrong -count 1
dial failed: failed to WebSocket dial: expected handshake response status code 101 but got 401 (http status 401)

$ wsprobe -token <token> -count 4
connected, negotiated subprotocol "spotdash.v1"
snapshot {"source":"clock","ts":"2026-09-14T10:01:07Z","data":{"iso":"2026-09-14T11:01:07+01:00","time":"11:01","seconds":7,"date":"Mon 14 Sep","sleep":false}}
update   {"source":"clock","ts":"2026-09-14T10:01:08Z","data":{"iso":"2026-09-14T11:01:08+01:00","time":"11:01","seconds":8,"date":"Mon 14 Sep","sleep":false}}
update   {"source":"clock","ts":"2026-09-14T10:01:09Z","data":{"iso":"2026-09-14T11:01:09+01:00","time":"11:01","seconds":9,"date":"Mon 14 Sep","sleep":false}}
update   {"source":"clock","ts":"2026-09-14T10:01:10Z","data":{"iso":"2026-09-14T11:01:10+01:00","time":"11:01","seconds":10,"date":"Mon 14 Sep","sleep":false}}
```

Snapshot first, then one message per second, all in the documented
`{source, ts, data}` shape with an RFC3339 timestamp. Unauthorised connections
are refused at the handshake, so they never reach a live socket.

### The clock face live, which item 5 deferred

```
$ chrome --headless=new --window-size=480,480 \
    --screenshot=live-clock.png "http://127.0.0.1:8765/?token=<token>"
```

The clock face rendered `11:01`, `Mon 14 Sep`, with the seconds arc filled to
roughly three quarters and a teal connection dot. The status face rendered
`clock ok 1s`, `up 48s`, `link live`.

### A slow close, found by a test being slow

Every socket test took exactly 5.00 seconds, which is a timeout rather than
work:

```
--- PASS: TestSocketAcceptsTheSessionCookie (5.00s)
--- PASS: TestSocketSendsTheFullStateOnConnect (5.00s)
--- PASS: TestTwoClientsBothReceiveUpdates (10.00s)
ok      github.com/afrugalpenguin/spotdash/agent/internal/server   35.736s
```

The panel only listens, so the handler never read from the connection, and a
connection that is never read from never processes control frames. The client's
close was therefore never acknowledged until it timed out. The same defect means
a device that vanishes from wifi is never noticed, since nothing reads the
frames that would reveal it.

Fixed with `CloseRead`, which drains incoming frames and cancels the handler
context when the peer goes away, plus a thirty second keepalive ping so a silent
dead connection is detected rather than held open.

```
--- PASS: TestSocketAcceptsTheSessionCookie (0.00s)
--- PASS: TestSocketSendsTheFullStateOnConnect (0.00s)
--- PASS: TestTwoClientsBothReceiveUpdates (0.00s)
ok      github.com/afrugalpenguin/spotdash/agent/internal/server   0.725s
```

### One measurement artifact, checked rather than assumed

A first screenshot showed `clock ok 7s` for a source that polls every second.
That is headless Chrome: `--virtual-time-budget` fast-forwards `Date.now()`
while socket messages still arrive in real time, so the rendered age inflates.
Rerun with a shorter budget it reads `1s`, and `wsprobe` shows timestamps one
second apart.

It did surface a real risk for the hardware, filed as issue 12: the age is the
browser clock minus the agent timestamp, and the Echo Spot has no battery-backed
real time clock, so a skewed device clock would render misleading ages on the
one face whose job is to be trustworthy.

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

Status: verified on 2026-09-14.

### Tests

```
$ go vet ./... && gofmt -l .
$ go test -race -count=1 ./...
?       .../agent/cmd/spotdash                    [no test files]
ok      .../agent/internal/config                 1.203s
ok      .../agent/internal/logging                1.173s
ok      .../agent/internal/server                 1.934s
ok      .../agent/internal/sources                1.639s
ok      .../agent/internal/sources/clock          1.142s
?       .../agent/internal/sources/partial        [no test files]
ok      .../agent/internal/sources/telemetry      1.591s
ok      .../agent/internal/state                  1.343s
?       .../agent/web                             [no test files]
```

### The GPU, cross-checked against nvidia-smi

Both read the same card seconds apart:

```
$ nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv
NVIDIA GeForce RTX 4070 Ti SUPER, 22 %, 3249 MiB, 16376 MiB, 30, 22.12 W

$ wsprobe -token <token>        # the agent's own reading
name             NVIDIA GeForce RTX 4070 Ti SUPER
utilisation      17 %
vram used        3564 MiB
vram total       16376 MiB
temperature      30 C
power            22.121 W
```

Name, VRAM total, temperature and power match exactly. Utilisation and VRAM used
differ because both are volatile and the two samples are seconds apart, which is
the expected result rather than a discrepancy to explain away.

The full reading over the socket, on a machine with 24 logical cores:

```
{"source":"telemetry","ts":"2026-09-14T10:26:53Z","data":{
  "cpu":{"percent":3.757693553611921,"per_core":[6.92,5.42,6.97,6.20,16.27,...]},
  "ram":{"used_bytes":17221271552,"total_bytes":33455644672,"percent":51.47493560754183},
  "disks":[{"mount":"C:","used_bytes":1742788804608,"total_bytes":1999323000832,"percent":87.1689468826575}],
  "gpu":{"name":"NVIDIA GeForce RTX 4070 Ti SUPER","percent":17,"vram_used_bytes":3748651008,
         "vram_total_bytes":17171480576,"vram_percent":21.83068018746947,
         "temperature_c":30,"power_watts":21.84}}}
```

### The degraded path

Exercised through the whole agent, not only against a fake system reader: the
real registry runner, the real state store, the real HTTP server, and a real
WebSocket client, with only the GPU reader replaced by one that fails the way a
missing driver does.

```
$ go test ./internal/sources/telemetry/ -run 'TestAgentKeeps|TestDegradedReading' -v
--- PASS: TestAgentKeepsRunningWhenNVMLIsUnavailable (0.00s)
--- PASS: TestDegradedReadingStillCarriesTheMachineOverTheSocket (0.00s)
```

Those assert what the acceptance asked for: `/health` reports `telemetry` as
`degraded` with the NVML reason in `last_error`, the socket still carries real
per core CPU, RAM and at least one disk, and `gpu` serialises as `null` rather
than an empty object that the panel would render as genuine zeroes.

The DLL rename variant was not performed. It needs administrator rights against
`C:\Windows\System32` and would briefly break GPU monitoring for everything else
on the machine, so it is not something to do unasked. Stubbing is the option the
plan allowed, and stubbing only the GPU reader keeps every other layer real.

### Two findings that changed the build

**go-nvml cannot build on Windows.** It loads the library through `dlfcn.h`,
which is POSIX:

```
$ go test ./internal/sources/telemetry/
# github.com/NVIDIA/go-nvml/pkg/dl
...\go-nvml@v0.13.4-0\pkg\dl\dl.go:26:11: fatal error: dlfcn.h: No such file or directory
   26 | // #include <dlfcn.h>
```

There are no build tags guarding it, so this is not a configuration problem. The
replacement is about a hundred lines binding `nvml.dll` directly through
`windows.NewLazySystemDLL`, which resolves only from the system directory so a
stray `nvml.dll` beside the binary cannot be loaded instead.

**That removes cgo from the project entirely**, which settles the question
carried forward from section 0:

```
$ CGO_ENABLED=0 go build -o spotdash-nocgo.exe ./cmd/spotdash
builds with CGO_ENABLED=0

$ # scan the produced binary for mingw runtime references
mingw runtime references: 0
nvml referenced: True
```

The agent is a single static binary with no runtime dependency on the compiler's
DLLs, and it references `nvml.dll` only as a lazy runtime load. The mingw
toolchain from section 0 is still worth having, because `go test -race` needs
it, but the shipped artefact no longer does.

`go vet` also caught a real issue in the first version of the binding: reading
`nvmlErrorString` meant converting a pointer into memory the Go runtime does not
own. The return codes are stable API, so they are mapped locally instead, which
is both safer and more predictable.

## 7. Telemetry face

Status: verified on 2026-09-14.

### Tests

Written before the code this time, unlike the JavaScript in item 5.

```
$ cd web && node --test app.test.js telemetry.test.js
tests 28
pass 28
fail 0
```

The threshold tests are the ones that matter, and they are written around the
boundaries rather than through the middle: 80 is ok and 80.1 is amber, 95 is
amber and 95.1 is red, 83C is ok and 83.1C is red. A gauge that stays calm while
a card sits at 97 percent is worse than no gauge.

There is also a test that a missing reading is neither ok nor alarming but a
third thing, absent. An unavailable GPU rendering as a calm empty gauge would be
a lie; rendering it as a red alarm would be a different lie.

```
$ go vet ./... && gofmt -l .
$ go test -race -count=1 ./...
ok      .../agent/internal/server                 1.9s
ok      .../agent/internal/sources                1.510s
ok      .../agent/internal/sources/clock          1.154s
ok      .../agent/internal/sources/telemetry      1.465s
ok      .../agent/internal/state                  1.341s
```

### The thresholds on screen

Rendered at 480x480 and reviewed as screenshots, not assumed.

Under load, with CPU 91, RAM 84, GPU 99, VRAM 97 and the card at 86C: the CPU
and RAM quadrants render amber, the GPU and VRAM quadrants red, and the centre
temperature red at `86` with `285 W` beneath it. Every threshold in the plan
lands on the colour it should.

Idle, against the real agent and the real card:

```
$ chrome --headless=new --window-size=480,480 \
    --screenshot=tele-live.png "http://127.0.0.1:8765/?token=<token>&face=telemetry"
```

rendered `cpu 4%`, `ram 52%`, `gpu 15%`, `vram 22%`, `29` degrees and `20 W`,
with all four quadrant arcs teal. Those values came from the running agent
reading the actual machine.

### The degraded payload

With `gpu` null, the face renders `cpu 13%` and `ram 51%` live in teal, the GPU
and VRAM readouts as a dimmed `n/a` over empty tracks, the centre as `n/a`, and
the reason stated as `no gpu reading` in amber.

Stating the reason matters. Two empty gauges alone would leave the viewer to
infer why, and the most natural inference, that the GPU is simply idle, is
wrong.

### The rim, again

The gauges hang on the same geometry the clock sweeps for seconds and the status
face splits per source, so the three faces read as one instrument rather than
three unrelated screens. Adding the telemetry face needed one new function in
the rim module and nothing else.

## 8. Tray integration and graceful shutdown

Status: verified on 2026-09-14.

### Tests

The lifecycle lives in `internal/app` precisely so it can be driven without a
desktop session, and the tray is a thin caller of it.

```
$ go test -race -count=1 ./internal/app/ -v
--- PASS: TestStartServesHealth
--- PASS: TestStartRefusesAnInvalidConfig
--- PASS: TestStopClosesTheListener
--- PASS: TestStopIsSafeToCallTwice
--- PASS: TestReloadAppliesANewToken
--- PASS: TestReloadWithAnInvalidConfigKeepsServing
--- PASS: TestReloadRejectsAnUnknownSourceAndKeepsServing
--- PASS: TestReloadBeforeStartIsAnError
--- PASS: TestOpenURLTargetsLoopbackAndCarriesTheToken
--- PASS: TestSourcesRunAfterStart
ok      github.com/afrugalpenguin/spotdash/agent/internal/app    1.701s
```

`TestReloadWithAnInvalidConfigKeepsServing` is the one that matters. Someone
mistypes a key in a running agent's config, and the agent has to say so and
carry on with what it already had rather than exiting and taking the panel dark.
The test asserts both halves: the error names the offending key, and the
previous token still works afterwards.

`TestOpenURLTargetsLoopbackAndCarriesTheToken` covers two things that would each
make the menu item useless: the shipped config listens on `0.0.0.0`, which a
browser cannot open, and a URL without the token lands on a 401.

### Graceful shutdown, with a real Ctrl+C

The obvious approach does not test anything:

```
$ kill -TERM <pid>      # from an MSYS shell
```

MSYS `kill` against a native Windows process calls `TerminateProcess`, which
never reaches Go's signal handler. The process dies, the test passes, and
nothing about graceful shutdown has been demonstrated.

The genuine path on Windows is a console control event. The harness starts the
agent as a child sharing its console, raises `CTRL_C_EVENT` on that console, and
keeps a handle so it can read the exit code. It sets its own ignore handler
*after* starting the child, because that flag is inherited: setting it first
makes the agent ignore the event too, which is a false pass that this harness
produced before the ordering was fixed.

```
serving before the event: True
exited: yes
exit code: 0
port 8765: released
```

```
$ tail -4 spotdash.log
level=INFO msg="shutdown requested"
level=DEBUG msg="source stopped" source=clock
level=DEBUG msg="source stopped" source=telemetry
level=INFO msg="stopped cleanly"
```

Exit code zero, both source goroutines stopped, the listener released, and no
error line. The same run was repeated with the tray enabled, which exercises the
systray event loop and the path where a signal takes the tray down so the
process is not left alive with nothing to serve:

```
[no-tray]   serving: True   exited: yes   port 8765 released   final log: stopped cleanly
[with-tray] serving: True   exited: yes   port 8765 released   final log: stopped cleanly
```

### The tray itself

The icon, the three menu items and their click handlers are the one part that
cannot be driven from a test, which is why there is as little code there as
possible: every menu item is a single call into `internal/app`. The tray path
was exercised end to end above, so systray starts, runs, and shuts down cleanly
in a real desktop session.

Clicking the items was not automated. `Open UI` and `Reload config` both call
methods that are covered by the tests above.

### Running without a desktop

`-no-tray` runs the agent as a plain console process. It exists because a tray
needs a desktop session and the verification above needs a process it can drive,
and it is the flag to use when running the agent from a terminal.

## 9. Shell in the 480x480 emulator

Status: verified on 2026-09-14.

### The AVD

```
$ cd shell\tools
$ .\avd.ps1 -CreateOnly
spotdash_480 configured at 480x480, API 30, 1024 MB

$ adb shell wm size
Physical size: 480x480
$ adb shell wm density
Physical density: 240
$ adb shell getprop ro.build.version.sdk
30
```

The emulated display is also set circular, so content straying outside the
inscribed circle disappears here exactly as it would on the device rather than
being discovered later.

Two details the script has to get right, both found the hard way:

- `avdmanager` asks about a hardware profile on stdin. Answering from a
  PowerShell pipeline sends UTF-16 with a byte order mark, and `avdmanager`
  rejects it with `Error: ?no is not a valid reply`. The answer goes through
  `cmd` instead.
- `local.properties` is a Java properties file, so a Windows path needs escaped
  backslashes or forward slashes. A raw path fails the build with
  `The filename, directory name, or volume label syntax is incorrect`, which
  names neither the file nor the setting.

### Build and install

```
$ cd shell
$ .\gradlew assembleDebug
BUILD SUCCESSFUL in 28s
33 actionable tasks: 33 executed

$ adb install -r app\build\outputs\apk\debug\app-debug.apk
Success
```

### Three defects the emulator caught

**The launcher crashed on start.** `goFullscreen()` ran before `setContentView`,
so the decor view did not exist and the insets controller was null:

```
FATAL EXCEPTION: main
java.lang.NullPointerException: Attempt to invoke virtual method
  'android.view.WindowInsetsController com.android.internal.policy.DecorView.getWindowInsetsController()'
  on a null object reference
    at dev.spotdash.shell.PanelActivity.goFullscreen(PanelActivity.kt:270)
    at dev.spotdash.shell.PanelActivity.onCreate(PanelActivity.kt:49)
```

This one is worth noting for what it would have meant on the real device: the
shell is the HOME launcher, so a crash on start leaves a device with no
launcher at all.

**The panel rendered at 1.5x and fell off the glass.** CSS pixels are density
independent and this display is 240dpi, so the panel's fixed 480 CSS pixel
layout became 720 physical pixels and only two thirds of it was visible. The
shell now computes an initial scale from the real display width, and the page no
longer pins `initial-scale=1`, so the panel fits whatever density it lands on.
That matters because the Echo Spot's density need not match the emulator's.

**A stray amber line ran down the middle of the panel.** A tap leaves the zone
button focused and the WebView draws its own focus ring; the circular clip hides
three of its four edges, leaving the inner vertical edge visible as a line
through the centre. Suppressed for touch focus and kept for keyboard focus,
which is navigation rather than an artefact of touching the glass.

None of the three would have been found without running it.

### The panel running in the shell

The clock face rendered at 480x480 with the seconds arc and a teal connection
dot, meaning the WebSocket was connected to the agent across the emulator's host
route. Tapping the right half advanced to the telemetry face, which rendered
live host values: `cpu 7%`, `ram 71%`, `gpu 2%`, `vram 23%`, `32` degrees,
`44 W`.

The settings screen opened on a genuine three second press:

```
$ adb shell input swipe 240 240 240 240 3500
```

and the agent URL and token were typed into it and saved, after which:

```
I spotdash: settings saved, reloading
I spotdash: loading the panel
I spotdash: page loaded
```

### The fallback, and recovery

The agent was stopped at 12:55:07. The shell noticed immediately, waited out the
threshold, showed the fallback, and began retrying:

```
12:55:07 W spotdash: agent unreachable: failed to connect to /10.0.2.2 (port 8765)
                     from /10.0.2.16 (port 46576) after 4000ms
12:55:47 I spotdash: retrying the panel
12:55:57 I spotdash: retrying the panel
```

The fallback screen showed the heading, the configured URL
`http://10.0.2.2:8765`, and the real connection error rather than a generic
message. Noticing at once but only showing the fallback after thirty seconds is
deliberate: a restarting agent is back within a second or two, and flashing a
fallback at every restart would be worse than briefly showing a stale dashboard.

With the agent back, recovery needed no intervention:

```
12:56:03 I spotdash: agent reachable again
12:56:03 I spotdash: loading the panel
12:56:03 I spotdash: page loaded
```

### How the shell knows

The shell polls `/health` natively rather than asking the page. That keeps the
bridge at exactly the four methods specified, and it means the fallback still
works when the WebView itself is what has failed, which is the case where asking
the page would be useless. `/health` is unauthenticated precisely so it stays
usable when everything else is broken, and this is the first thing to actually
rely on that.

### One false alarm worth recording

A screenshot mid-verification showed an amber connection dot and frozen gauges,
which looked like the socket failing to re-establish after recovery. It was not:
the agent had been stopped at that moment to switch its log level to debug. The
console message that made it look real came from the old app instance timing out
against a host port that genuinely was not listening:

```
I chromium: [INFO:CONSOLE] "WebSocket connection to 'ws://10.0.2.2:8765/ws' failed:
             Error in connection establishment: net::ERR_CONNECTION_TIMED_OUT"
```

Checked properly, the agent reports one socket open and holding:

```
$ grep -c "websocket client connected" agent.log     # 2
$ grep -c "websocket client disconnected" agent.log  # 1
currently open: 1
```

and the panel renders the clock live with a teal connection dot.

## 10. End to end

Status: verified on 2026-09-14.

Every section above records work verified on the machine it was built on. This
one starts from a fresh clone, so nothing carried over from the state that
machine happened to be in.

### Clone

```
$ git clone <repo> spotdash-clean-checkout
$ cd spotdash-clean-checkout && git log --oneline | head -1
235f7c1 fix(web): avoid css newer than the device webview (#23)

$ ls agent/
cmd  config.example.json  go.mod  go.sum  internal  run.ps1  web

config.json present? no
binary present? no
```

No secret and no build output came with the clone, which is what `.gitignore` is
there for.

### It refuses to start before it is configured

```
$ go build -o spotdash.exe ./cmd/spotdash
$ ./spotdash.exe
spotdash: config file not found: looked in [...\agent\config.json]. Copy config.example.json to config.json, or pass -config
exit=1

$ cp config.example.json config.json     # then blank the token
$ ./spotdash.exe
spotdash: in ...\agent\config.json: "token" is required and must not be empty: the agent will not serve data without a shared secret
exit=1
```

The example config ships with a placeholder token rather than a working one, so
a fresh clone cannot accidentally serve with a secret that is in public version
control.

### Tests

```
$ go vet ./... && gofmt -l .
both clean

$ go test -race -count=1 ./...
?       .../agent/cmd/spotdash                [no test files]
ok      .../agent/internal/app                1.685s
ok      .../agent/internal/config             1.221s
ok      .../agent/internal/logging            1.173s
ok      .../agent/internal/server             1.903s
ok      .../agent/internal/sources            1.546s
ok      .../agent/internal/sources/clock      1.157s
?       .../agent/internal/sources/partial    [no test files]
ok      .../agent/internal/sources/telemetry  1.640s
ok      .../agent/internal/state              1.343s
?       .../agent/internal/tray               [no test files]
?       .../agent/web                         [no test files]

$ cd web && node --test app.test.js telemetry.test.js
tests 28   pass 28   fail 0
```

### Serving

With a real token and `127.0.0.1:8799`:

```
$ curl http://127.0.0.1:8799/health
{"version":"1.0.0-clean","uptime_seconds":8.1021862,"sources":{
  "clock":{"status":"ok","last_update":"2026-09-14T12:12:08Z"},
  "telemetry":{"status":"ok","last_update":"2026-09-14T12:12:08Z"}}}

GET / no token       -> 401
GET / with token     -> 200
GET / browser style  -> 200
```

The socket, from a client outside any browser:

```
$ wsprobe -addr ws://127.0.0.1:8799/ws -token <token> -count 3
connected, negotiated subprotocol "spotdash.v1"
snapshot {"source":"clock","ts":"2026-09-14T12:12:08Z","data":{...,"time":"13:12","seconds":8,...}}
update   {"source":"telemetry","ts":"2026-09-14T12:12:08Z","data":{"cpu":{"percent":3.6281179138321997,...
update   {"source":"clock","ts":"2026-09-14T12:12:09Z","data":{...,"seconds":9,...}}
```

All three faces were then rendered at 480x480 from the clean build. The status
face reported both sources `ok`, `up 17s`, and `link live`.

### The shell, from the same clone

```
local.properties present? no

$ .\gradlew assembleDebug
BUILD SUCCESSFUL
33 actionable tasks: 33 executed

$ ls app\build\outputs\apk\debug\app-debug.apk
9711395 bytes
```

A clean checkout needs only `ANDROID_HOME` set. `local.properties` is gitignored
and is not required, which is worth knowing because when it *is* present and
wrong the build fails with `The filename, directory name, or volume label syntax
is incorrect`, naming neither the file nor the setting.

The APK carries no native libraries, so the build verified on an x86_64 emulator
runs unchanged on the device's ARM chip.

### One blemish, fixed

The not-configured message listed the same path twice, because the binary
usually sits in the working directory and both candidates resolve to it. It read
as a bug in the message rather than a missing file. Now deduplicated.

### What phase 1 does not cover

Everything that needs the hardware. `docs/device.md` is the bring-up list, and
the one known defect waiting for it is issue 12: source ages are the device
clock minus the agent timestamp, and the Echo Spot has no battery backed real
time clock.
