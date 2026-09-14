# Architecture

## Shape of the system

```
+-------------------------------------------------------------+
|  Windows desktop                                             |
|                                                              |
|   +------------------------------------------------------+   |
|   |  agent (single Go binary, system tray)               |   |
|   |                                                      |   |
|   |   sources/          state/            server/        |   |
|   |   +---------+       +---------+       +---------+    |   |
|   |   | clock   |--\    |         |       | /health |    |   |
|   |   +---------+   >-->|  store  |------>| /ws     |    |   |
|   |   |telemetry|--/    |         |       | /  web  |    |   |
|   |   +---------+       +---------+       +---------+    |   |
|   |        ^                 |                  ^        |   |
|   |    registry          broadcast          embed FS     |   |
|   +------------------------------------------------------+   |
+---------------------------|----------------------------------+
                            | LAN, plain HTTP, bearer token
                            v
              +---------------------------------+
              |  Echo Spot, LineageOS 18.1      |
              |                                 |
              |   shell (Kotlin, one Activity)  |
              |   immersive WebView to agent UI |
              |   window.shell bridge           |
              +---------------------------------+
```

The agent owns everything that can fail in an interesting way. The shell owns
nothing but the glass.

## Agent

### Packages

| Package             | Responsibility                                                              |
| ------------------- | --------------------------------------------------------------------------- |
| `internal/config`   | Load and validate `config.json`. Fail closed.                                |
| `internal/sources`  | The `Source` contract, the registry that runs them, one package per source.  |
| `internal/state`    | Aggregated latest-value store plus fan-out of change notifications.          |
| `internal/server`   | HTTP routing, auth middleware, WebSocket hub, embedded static files.         |
| `internal/tray`     | System tray icon and menu.                                                   |
| `cmd/spotdash`      | Wiring and lifecycle. Nothing else.                                          |

### Configuration

`config.json` sits next to the binary. Keys:

| Key         | Type   | Notes                                                         |
| ----------- | ------ | ------------------------------------------------------------- |
| `listen`    | string | `host:port`. Default `0.0.0.0:8765`.                           |
| `token`     | string | Shared secret. Required. An empty token is a startup failure.  |
| `log_level` | string | `debug`, `info`, `warn`, or `error`.                           |
| `sources`   | object | Source name to settings. Every source has `enabled` and `interval_ms`; sources may add their own keys. |

Validation is strict and total. A missing file, invalid JSON, an unknown
top-level key, an empty token, an unparseable `listen`, an unknown `log_level`,
or a non-positive `interval_ms` on an enabled source all cause the process to
exit non-zero with a message naming the offending key. The agent never starts in
a half-configured state.

Unknown top-level keys are rejected rather than ignored, because a mistyped key
that is silently dropped produces a config that does not mean what it appears to
mean. Keys inside a source block are the exception: they are handed to that
source untouched, which is what lets a new source add settings without changing
the config package.

The log file is written to the directory holding the resolved config file. In
normal use that is the directory holding the binary.

### The source contract

```go
type Source interface {
    Name() string
    Poll(ctx context.Context) (any, error)
    Interval() time.Duration
}
```

That is the whole contract. A source knows how to produce one value and how
often. It does not know about HTTP, WebSockets, the state store, logging policy,
or the other sources.

The registry supplies everything else:

- One goroutine per enabled source.
- A panic in `Poll` is recovered and recorded as an error, not a crash.
- `Poll` runs under a context with a deadline derived from the interval.
- Consecutive failures trigger exponential backoff up to a ceiling, so a source
  whose dependency is gone does not spin.
- Success writes the value into the state store and marks the source `ok`.
  Failure marks it `degraded` and retains the last good value and the last error
  string.

Adding a source is one new package that implements the interface, plus one line
in the factory table. Nothing else in the agent changes. This is the property
that makes the later faces (Spotify, calendar, weather, voice) cheap.

A source package cannot import the registry package without a cycle, so each
source constructor returns its own concrete type and a small generic adapter in
the registry widens it to the interface. That keeps the table one line per
source.

A source named in config with no implementation behind it is a startup error,
including when it is disabled. Starting anyway would leave a face that never
populates with nothing on screen explaining why, and a typo in a disabled block
is the likeliest way a working source gets silently switched off.

Sources are constructed before the listener opens, so a source that cannot be
built stops the agent instead of degrading forever. Validation that can happen
once, such as parsing the clock sleep window, happens at construction rather
than on every poll.

### Source status

| Status     | Meaning                                                    |
| ---------- | ---------------------------------------------------------- |
| `ok`       | Last poll succeeded.                                       |
| `degraded` | Enabled, but the last poll failed, or it is running with reduced capability such as telemetry without NVML. |
| `disabled` | Turned off in config. Not polled, not broadcast.           |

Degradation is deliberately not fatal and deliberately visible. A telemetry
source on a machine with no usable NVML still reports CPU, RAM, and disk, with
the GPU fields null, and marks itself `degraded` with the NVML error attached.

The NVML binding is the one part of the agent that needs cgo, so a C compiler is
a build requirement even though nothing else uses one. Two consequences follow.
Go defaults `CGO_ENABLED` to 0 when it cannot find a compiler on PATH, which
turns GPU telemetry off with no error at build time, so the build environment is
checked rather than assumed. And a cgo-linked binary can acquire a runtime
dependency on the compiler's own DLLs, which would defeat the single binary
goal; the produced binary is checked for that and linked statically if needed.

### State and transport

The state store holds the latest value per source with its timestamp and status.
It is the single source of truth for both `/health` and `/ws`, so the debug view
and the live view can never disagree.

| Endpoint       | Auth  | Behaviour                                                        |
| -------------- | ----- | ---------------------------------------------------------------- |
| `/health`      | none  | JSON: uptime, version, per-source status, last update, last error. |
| `/ws`          | token | Full state snapshot on connect, then one message per source update. |
| `/` and static | token | The embedded web UI, served from `embed.FS`.                      |

### How a browser authenticates

A bearer header is the right mechanism for a programmatic client and impossible
for a browser: nothing can set a header on a navigation, and the stylesheet and
modules the page then requests carry neither a header nor a query string. A
panel behind a header alone can never load itself.

So the token is accepted three ways, in this order:

1. `Authorization: Bearer <token>`, for anything programmatic.
2. `?token=<token>` on the request, which is how the panel URL is opened once.
3. A `spotdash_session` cookie, which the second case sets on success.

The cookie is `HttpOnly` so page scripts cannot read the token back out,
`SameSite=Strict`, and carries no `Expires` or `Max-Age`, so it lives for the
browser session and is never written to disk. A request authenticated by header
is deliberately not given a cookie: a programmatic client should not be handed
browser state it never asked for.

`SameSite=Strict` plus the absence of any state-changing endpoint is what stands
in for CSRF protection. There is nothing to forge a request against.

The cost of this is that the token appears in one URL, where a proxy or an
access log could record it. That is accepted on the same basis as the rest of
the phase 1 posture, and the page removes it from the address bar immediately.

WebSocket message shape:

```json
{ "source": "telemetry", "ts": "2026-09-14T10:04:11Z", "data": {} }
```

The snapshot sent on connect is a sequence of those same messages, one per
source, so the client has exactly one code path for handling state. A source
that has never polled is left out of the snapshot: an empty message would have
the panel render a blank value as though it were a reading.

Only successful polls are broadcast. A failure changes status, which the client
reads from `/health`, and leaves the last good reading in place.

### Backpressure

A source goroutine writes into the store, and the store fans out to subscribers
without ever blocking. Each subscriber has a small buffer sized for a brief
stall, a garbage collection pause or a frame the device spent elsewhere. A
subscriber that fills it is dropped and its channel closed.

Dropping is deliberate rather than skipping messages. A dropped client
reconnects and is handed a fresh snapshot, so it is never quietly stale. A
client that silently missed messages has no way to know it did.

This is the property that keeps one wedged panel from stalling every source in
the agent.

### Liveness

The panel only listens, so the server never reads application messages from a
connection. A connection that is never read from also never processes control
frames, which means a client's close is not acknowledged until it times out and
a client that has vanished is never noticed at all.

So the handler drains and discards incoming frames, which both acknowledges
closes promptly and cancels the handler when the peer goes away, and pings a
silent connection every thirty seconds. A kiosk on wifi can disappear without
closing anything, and without the ping there would be nothing to detect that.

`/health` is unauthenticated on purpose. It is the thing you curl when nothing
works, and it exposes status and error strings only, never source data and never
the token.

## Web UI

Plain HTML, CSS, and vanilla JavaScript served from the binary. No framework and
no build step, because the target browser is a WebView on a 2017 MediaTek SoC and
every kilobyte and every parse is real time on that device.

### Faces

A face is a module that exports:

```js
export function render(container, state) {}
export function onState(source, data) {}
```

`render` builds the DOM once. `onState` mutates what already exists. The face
manager shows exactly one face at a time, switches on a tap in the left half
(previous) or right half (next), and honours a `?face=` query parameter on load.

Faces are independent. A face that throws is contained and reported rather than
taking the panel down.

Phase 1 faces: `telemetry`, `clock`, `status`. The `status` face lists every
source with its status, last update, and last error, along with agent uptime and
WebSocket connection state. It is the debug face and the fallback whenever the
WebSocket is down, so there is always something truthful on screen.

### Layout

The root is a 480x480 square with a circular `clip-path` on a dark background.
Type is large and high contrast, sized for a viewing distance of about 60 cm.
Anything near the corners is invisible on the real device, so content stays
inside the inscribed circle.

### Token handling in the UI

The token arrives once as `?token=` on first load. The page reads it, removes it
from the visible URL with `replaceState`, and holds it in memory only. It is
never written to `localStorage` or `sessionStorage`, and the only place it goes
afterwards is the WebSocket handshake.

It travels to the socket as a subprotocol value rather than a query parameter,
because a browser cannot set headers on a WebSocket and a query parameter would
put the secret somewhere that gets logged.

### Where status comes from

The socket carries readings. Status, uptime, and last error come from `/health`,
which the panel polls every five seconds.

Splitting it that way is deliberate. `/health` needs no auth and keeps answering
when the socket is down, which is exactly the moment the status face has to be
truthful about what is wrong. A status view that goes blank when the connection
drops is a status view that fails when you need it.

### Reconnection

The WebSocket client reconnects with exponential backoff and jitter. Connection
state is rendered as a small indicator present on every face, so a stale panel is
always distinguishable from a live one.

## Shell

Single Activity, `minSdk` and `targetSdk` 30. Fullscreen immersive, screen kept
on, no title bar and no system bars. It declares the `HOME` and `DEFAULT` intent
categories so LineageOS can set it as the default launcher.

The agent URL and token live in `EncryptedSharedPreferences`, entered on a
settings screen opened by a three second long press anywhere on the display. That
gesture is the only UI the shell has beyond the WebView, because there is no
other input on the device.

The shell injects the token as a query parameter on the initial page load only.
After that the page holds it in memory and the shell forgets about it.

### JavaScript bridge

`window.shell` exposes exactly four methods in phase 1:

| Method                 | Purpose                        |
| ---------------------- | ------------------------------ |
| `setBrightness(0-255)` | Panel brightness.              |
| `screenOff()`          | Blank the panel.               |
| `screenOn()`           | Wake the panel.                |
| `keepAwake(bool)`      | Hold or release the wake lock. |

Each method is a no-op when the required permission is missing, and logs why.
That keeps the same UI working unchanged in an emulator, where none of these are
available.

### Failure behaviour

If the WebView fails to load, or the WebSocket has been down for more than 30
seconds, the shell shows a native fallback screen with the configured agent URL
and the last error, and retries every 10 seconds. The fallback is native rather
than web because the web layer is exactly what is in question at that moment.

## Security posture for phase 1

The agent binds to the LAN and requires `Authorization: Bearer <token>` on every
endpoint except `/health`. There is no TLS.

This means anyone with LAN access who can observe traffic can read the token and
the dashboard data. That is an accepted risk for phase 1, on the reasoning that
the data is desktop telemetry and a clock, the network is a home LAN, and the
device is a fixed kiosk. It is written down here rather than left implicit so
that adding a second user, leaving the LAN, or adding a source with sensitive
data is understood as the trigger to revisit it.

Mitigations that are in scope now:

- The token is required and non-empty or the agent refuses to start.
- `/health` never returns source data or the token.
- The token is never persisted by the web UI and never returned to the URL.
- `config.json` is excluded from version control.

Explicitly out of scope for phase 1: TLS, per-client credentials, token rotation,
rate limiting, and any write path from the UI back to the agent. There are no
command endpoints, so a leaked token reads data and can do nothing else.

## Logging

Structured logging to stderr and to a rotating file next to the binary. Every
source poll is logged at debug level with its outcome and duration, so a
misbehaving source is diagnosable from the log alone without attaching anything
to a running process.

## Non-goals for phase 1

No Spotify, calendar, weather, voice, or command handling. No TLS. No installer
and no Windows service. No animation beyond simple transitions.
