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
| `internal/app`      | The lifecycle: start, reload, stop. Drivable without a desktop.              |
| `internal/tray`     | System tray icon and menu. A thin caller of `internal/app`.                  |
| `cmd/spotdash`      | Wiring. Nothing else.                                                        |

### Configuration

`config.json` sits next to the binary. Keys:

| Key         | Type   | Notes                                                         |
| ----------- | ------ | ------------------------------------------------------------- |
| `listen`       | string | `host:port`. Default `0.0.0.0:8765`.                           |
| `token`        | string | Shared secret. Required. An empty token is a startup failure.  |
| `log_level`    | string | `debug`, `info`, `warn`, or `error`.                           |
| `accent_color` | string | `"#rrggbb"`. Optional; absent keeps the stylesheet's own default. Normally set from the tray's Options page rather than hand-edited; see "Settings" below. |
| `sources`      | object | Source name to settings. Every source has `enabled` and `interval_ms`; sources may add their own keys. |

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

### A third outcome: partial results

A poll has two natural outcomes, a value or a failure, and telemetry has a
third. On a machine where the GPU cannot be read, CPU, RAM and disk are all
genuinely present and worth publishing, and the source is genuinely degraded.
Forcing that into either outcome loses something: a failure throws away a good
reading, a success hides a real problem.

So a source may return a value together with an error marked partial. The runner
stores and broadcasts the value, marks the source degraded, records the reason,
and does not count it as a failure for backoff, because nothing is failing and
polling less often would not bring the missing part back.

The marker lives in its own small package so a source can use it without
importing the registry that runs it, which would be an import cycle.

### GPU telemetry

NVML, NVIDIA's management library, is the only way to read utilisation, VRAM,
temperature and power. It ships with the driver as `nvml.dll`, and it is the
same interface `nvidia-smi` uses.

NVIDIA's own `go-nvml` cannot build on Windows: it loads the library through
`dlfcn.h`, which is POSIX, with no build tags guarding it. So `nvml.dll` is
bound directly through `windows.NewLazySystemDLL`, which resolves only from the
system directory, so a stray `nvml.dll` beside the binary or in the working
directory cannot be loaded in its place.

That binding is pure Go, which means the agent needs no cgo at all and ships as
a single static binary with no runtime dependency on a compiler's DLLs. A C
toolchain is still worth having for `go test -race`, but nothing shipped
requires one.

The library is loaded lazily and initialisation is retried on every read that
finds it unestablished. That covers what actually happens on a desktop: the
agent starts while the driver is updating, and the card appears a minute later.
Initialising once at construction would leave the GPU missing until the agent
was restarted.

Within a successful read, each field is read independently and one failure is
not fatal. Not every card reports power draw, and a driver can refuse a single
metric while serving the rest; losing the whole GPU over a missing wattage would
be the wrong trade.

### Spotify

Two providers, one `Reading` shape, selected by the required `mode` setting.
No default: running the mock unintentionally would be worse than refusing to
start.

- `mode: "mock"`, plays a configured track, no network.
- `mode: "api"`, real Spotify Web API.

**Auth**: Authorization Code with PKCE, no client secret. Settings:
`client_id`, `redirect_uri` (must match the Spotify app exactly), `state_file`.

- `GET /spotify/connect` starts a fresh attempt and redirects to Spotify.
  Requires the bearer token.
- `GET /spotify/callback` is the OAuth redirect target. Can't require the
  token, since a fresh browser tab has none. Protected instead by a
  single-use `state` value with a 10 minute expiry, the loopback-redirect
  model RFC 8252 describes.

Both routes are optional interfaces (`RouteProvider`, `OpenRouteProvider`) a
source can implement, the same pattern as `AssetProvider` for album art.

**Storage**: refresh token in `state_file`, not `config.json`. Config is
hand-edited; this file is agent-written. Atomic write, temp file then rename.
Mode 0600, though NTFS does not enforce POSIX permissions, so on Windows this
is no stronger than `config.json`'s existing exposure.

**Scope**: `user-read-currently-playing`, `user-read-playback-state`,
`user-modify-playback-state`. The write scope was added for playback control
below; an existing connection made before it must reconnect via
`/spotify/connect` to pick it up.

**Polling**: `GET /me/player/currently-playing`, default 5s interval. Access
token refreshed before expiry, or once on a 401. No content or a non-track
item means nothing playing, a success with an empty reading. Not connected or
revoked is a failure, with a `/spotify/connect` hint in the error.

**Art**: fetched once per track, cached to one file next to `state_file`,
served from the agent's own origin rather than a direct CDN hit from the
device. Re-fetched only when the track ID changes. The published `ArtURL`
carries the track ID as a query parameter (`/art/spotify?track=...`) rather
than the bare path: the file behind it is correctly re-downloaded on every
track change, but an unchanged URL gives neither the browser's own cache nor
the client's same-URL guard in `setArt()` any reason to treat the cover as
different, so without this the cover freezes on whichever track first set
it.

**Consistency after a skip**: Spotify's own `currently-playing` endpoint does
not reliably reflect a `next`/`previous` immediately, even though the
control call itself is accepted right away; measured live, anywhere from
under 200ms to over a second. `handleControl` flags this
(`expectingChange`), and the poll immediately after retries briefly
(`consistencyRetries`, `consistencyDelay`) rather than accepting a read that
is still the track from before the action. Bounded and best-effort: pause
and resume do not set the flag, since `Playing` is reflected immediately in
practice and there is no "which track" ambiguity for a retry to resolve.

**Layout**: `"fill"` or `"disc"`, default `"fill"`, set via config rather than
a URL parameter since the shell loads one fixed URL with no way to attach one.

### Calendar

Same two-provider shape as Spotify: `mode: "mock"` for a configured sample
event, `mode: "ics"` for a real feed. No OAuth: an ICS feed is a URL, which
Outlook publishes natively and any future replacement calendar only needs to
serve the same way.

The next-up event is the primary content: title, location (whatever the
feed's own `LOCATION` field says, e.g. "Microsoft Teams Meeting"), start
time, and a countdown that ticks locally between polls the same way spotify's
position does. Below it, a short agenda (`AgendaSize`, 3 total including the
primary) of what follows: title and start time only, no location or
countdown, since urgency and the rim colour stay keyed to the one primary
event. Still no multi-calendar merge, no editing.

**Parsing** (`ics.go`): a minimal hand-rolled RFC 5545 reader, not a library.
Reads `SUMMARY`, `LOCATION`, `DTSTART` (UTC, a named `TZID`, or floating
local time), and `RRULE`. `time/tzdata` is embedded so a named `TZID`
resolves without depending on the host machine having its own timezone
database, matching the single-static-binary design. All-day events are
excluded from next-up selection: "next up in N minutes" does not mean
anything for one.

**Recurrence** (`rrule.go`): expands the RFC 5545 shapes an actual person's
calendar uses, not the full spec: `FREQ` of `DAILY`/`WEEKLY`/`MONTHLY`/
`YEARLY`, `INTERVAL`, `COUNT`, `UNTIL`, `BYDAY` (plain weekday codes, weekly
only, e.g. a weekday standup), `BYMONTHDAY` (positive days, monthly only).
`nextOccurrence` walks forward from `DTSTART` (bounded by `recurrenceCap`,
500 occurrences) to find the first one after now, so a recurring event's
own `DTSTART`, almost always long in the past, is never itself mistaken for
"next up".

Ordinal `BYDAY` ("the third Thursday", `BYDAY=3TH`), negative
`BYMONTHDAY` ("the last day of the month"), `BYSETPOS`, `BYWEEKNO`,
`BYYEARDAY`, `WKST`, and sub-daily frequencies are unsupported and reported
as such by `parseRRule` rather than guessed at; `nextUpEvent` excludes an
event whose `RRULE` it cannot expand, the same conservative fallback as
before recurrence support existed, not a regression from it.

**Auto-switch**: the source itself decides urgency, not the client. Each
reading carries `urgent` (true once the event is within `notify_minutes` of
its `config.Source` block, default 15) and `show_seconds` (default 45). The
client (`app.js`) switches to the calendar face the moment a reading with a
new `urgent` event arrives, forces the sleep window off for the duration
even during the clock's configured sleep hours, holds for `show_seconds`,
then returns to whatever face was showing, unless the viewer has already
tapped away. Deciding "urgent" server-side keeps the threshold in one place
and out of the client entirely.

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

Phase 1 faces: `clock`, `telemetry`, `status`, in that tap order. The clock
comes first because it is what the panel shows most of the time, and status last
because it is the debug face. The `status` face lists every
source with its status, last update, and last error, along with agent uptime and
WebSocket connection state. It is the debug face and the fallback whenever the
WebSocket is down, so there is always something truthful on screen.

### The rim

Every face draws quantity on a circular track just inside the edge, and detail
in the centre. The clock sweeps seconds around it, the status face splits it
into one segment per source coloured by health, and the telemetry face hangs
four gauges on its quarters.

That is the one deliberately bold idea in the UI, and it is load bearing rather
than decorative: system health, track progress and machine load are all readable
from across the room without reading a word. It also means the faces read as one
instrument rather than three unrelated screens, and a new face gets the language
for free.

### Colour

The resting signal is a cool colour (`--live`, blue by default) and the alert
colours are warm, amber above 80 percent and red above 95, or above 83C for GPU
temperature. The cool resting state is chosen so that a warning is unmistakable
at a glance rather than a hue judgement.

`--live` is the panel's one accent colour token; every face's rim, connection
dot, gauge and border derives from it, so changing it is a single-source
change. Configurable via `accent_color` in `config.json`, normally set from
the settings page (tray: Options) rather than hand-edited. Applied live: the
value rides on the existing `/health` poll, so a saved change reaches an
already-open panel within one poll interval, no reload needed. See
"Settings" below.

A missing reading is a third state, not zero. An unavailable GPU renders as
absent, with the reason stated, because a calm empty gauge and a red alarm are
both wrong in different directions.

### Settings

`GET/POST /settings/accent`, authenticated the same as everything else that
is not `/health` or an OAuth callback. GET reports the currently configured
`accent_color` (empty if unset); POST validates a `"#rrggbb"` value, writes it
to `config.json` (`config.Save`, atomic, mirrors the pattern spotify's
`state_file` uses), and reloads.

The reload is deliberately not synchronous inside the POST handler: `Reload`
tears down and rebuilds the whole session, including the listener the POST
request itself arrived on. Calling it inline would have `Shutdown` wait for
this handler to return while the handler waits for `Reload` to return, a real
deadlock resolved only by the shutdown grace period force-closing the
connection before the response goes out. `time.AfterFunc` schedules the
reload a short beat after the response is sent instead.

The page itself (`settings.html`/`settings.js`) is an ordinary static file
under `agent/web`, served the same way the panel is. One field today, but the
route and the write-back mechanism are generic enough that a second setting
is an added field, not a restructure.

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

The WebSocket client reconnects with exponential backoff and jitter on a
`close` or `error` event. Connection state is rendered as a small indicator
present on every face.

That indicator is not the whole story: observed live, a connection can go
silently stale without either event firing, some sources still updating
while at least one stops, the indicator reporting "live" the entire time. Not
fully explained (WebView backgrounding is the leading suspect), so rather
than trying to prevent it, a watchdog detects and recovers from it: every
message and every socket open updates a last-heard timestamp, and a
10-second check closes the connection if 60 seconds pass with nothing heard
while the panel still believes it is live. Closing feeds into the same
`close` handler an actual disconnect would, so there is one recovery path,
not two.

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

If the WebView fails to load, or the agent has been unreachable for more than 30
seconds, the shell shows a native fallback screen with the configured agent URL
and the last error, and retries every 10 seconds. The fallback is native rather
than web because the web layer is exactly what is in question at that moment.

The shell polls `/health` itself to decide this, rather than asking the page.
That keeps the bridge at exactly the four methods above, and it means the
fallback still works when the WebView is the thing that has failed. `/health` is
unauthenticated precisely so it stays usable when everything else is broken.

The thirty second delay is deliberate. The shell notices a failure at once, but
a restarting agent is back within a second or two, and flashing a fallback at
every restart would be worse than briefly showing a stale dashboard.

### Scaling

The panel is a fixed 480 CSS pixel layout, and CSS pixels are density
independent. On a 240dpi display those become 720 physical pixels and two thirds
of the panel falls off the glass. The shell therefore computes an initial scale
from the real display width rather than assuming one, so the panel fits whatever
density it lands on. The Echo Spot's density need not match the emulator's.

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

## Lifecycle

Start, reload and stop live in `internal/app`, apart from the tray. A tray needs
a desktop session, which a test does not have, so putting the lifecycle behind
it would make the whole thing unverifiable. Every tray menu item is one call
into that package, and `-no-tray` runs the same agent as a plain console
process.

A reload replaces one running configuration wholesale, because `listen`, `token`
and the source set can all change. It is fail closed in the same way startup is,
and then some: the new config is loaded and its sources are built before
anything running is touched, so an invalid config leaves the agent exactly as it
was. Someone mistyping a key while the agent is running should be told, not have
the panel go dark.

If the new config validates but cannot be served, most likely because something
took the port in between, the previous configuration is restored rather than
leaving the agent down.

Shutdown stops accepting requests and closes open connections first, then stops
the sources that were feeding them, then waits for their goroutines. Ctrl+C and
tray Quit converge on that single path, and a signal also takes the tray down so
the process is not left alive with nothing to serve.

## Logging

Structured logging to stderr and to a rotating file next to the binary. Every
source poll is logged at debug level with its outcome and duration, so a
misbehaving source is diagnosable from the log alone without attaching anything
to a running process.

## Non-goals for phase 1

No Spotify, calendar, weather, voice, or command handling. No TLS. No installer
and no Windows service. No animation beyond simple transitions.
