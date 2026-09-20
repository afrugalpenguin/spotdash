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

Agent owns everything that can fail. Shell owns the glass.

## Agent

### Packages

| Package             | Responsibility                                                              |
| ------------------- | --------------------------------------------------------------------------- |
| `internal/config`   | Load/validate `config.json`. Fail closed.                                   |
| `internal/sources`  | `Source` contract + registry that runs them, one package per source.        |
| `internal/state`    | Latest-value store, fans out change notifications.                          |
| `internal/server`   | HTTP routing, auth, WebSocket hub, embedded static files.                   |
| `internal/app`      | Lifecycle: start, reload, stop. Drivable without a desktop.                 |
| `internal/tray`     | System tray icon/menu, thin caller of `internal/app`.                       |
| `cmd/spotdash`      | Wiring only.                                                                |

### Configuration

`config.json` sits next to the binary.

| Key         | Type   | Notes                                                         |
| ----------- | ------ | ------------------------------------------------------------- |
| `listen`       | string | `host:port`. Default `0.0.0.0:8765`.                           |
| `token`        | string | Shared secret, required, empty = startup failure.               |
| `log_level`    | string | `debug`, `info`, `warn`, `error`.                              |
| `accent_color` | string | `"#rrggbb"`. Optional. Set via tray Options page normally.      |
| `hidden_faces` | array  | Face titles left out of tap rotation. Can't hide every face or `clock`. Set via Options page. |
| `clock_style`  | string | `"digital"` or `"analogue"`, default `"digital"`. Set via Options page. |
| `sources`      | object | Name -> settings. Every source has `enabled`, `interval_ms`, plus its own keys. |

Validation is strict and total: missing file, invalid JSON, unknown top-level key, empty token, bad `listen`, unknown `log_level`, or non-positive `interval_ms` on an enabled source all exit non-zero naming the key. Unknown top-level keys are rejected (a typo shouldn't silently no-op); keys inside a source block pass through untouched so a source can add settings without touching config.

Log file lives next to the resolved config file.

### The source contract

```go
type Source interface {
    Name() string
    Poll(ctx context.Context) (any, error)
    Interval() time.Duration
}
```

A source knows how to produce one value and how often. Nothing about HTTP, WebSockets, state, logging, or other sources.

Registry supplies:

- One goroutine per enabled source.
- Panics in `Poll` recovered and recorded, not a crash.
- `Poll` runs under a deadline derived from the interval.
- Exponential backoff on consecutive failures, capped.
- Success writes to the state store, marks `ok`. Failure marks `degraded`, keeps last good value + last error.

Adding a source = one new package implementing the interface + one line in the factory table.

A source package can't import the registry (cycle), so each constructor returns its own concrete type and a small generic adapter widens it in the registry.

A source named in config with no implementation is a startup error, even disabled - a typo is the likeliest way a working source gets silently switched off. Sources are constructed before the listener opens, so a bad one stops the agent rather than degrading forever.

### Source status

| Status     | Meaning                                                    |
| ---------- | ---------------------------------------------------------- |
| `ok`       | Last poll succeeded.                                       |
| `degraded` | Enabled but last poll failed, or running with reduced capability (e.g. telemetry without NVML). |
| `disabled` | Off in config. Not polled, not broadcast.                  |

### A third outcome: partial results

Telemetry can be genuinely fine on CPU/RAM/disk but missing GPU. Forcing that into pure success/failure loses something, so a source may return a value plus a partial-marked error: stored and broadcast, source marked `degraded`, not counted toward backoff (nothing's actually failing, and polling less won't fix a missing GPU). Marker lives in its own package to avoid the registry import cycle.

### GPU telemetry

NVML (`nvml.dll`, same lib `nvidia-smi` uses) is the only way to read utilisation/VRAM/temp/power. NVIDIA's own `go-nvml` won't build on Windows (uses `dlfcn.h`, POSIX-only, no build tags), so `nvml.dll` is bound directly via `windows.NewLazySystemDLL`, which only resolves from the system directory. Pure Go, no cgo needed for the shipped binary (a C toolchain is still needed for `go test -race`).

Loaded lazily, init retried on every read that finds it unestablished - the agent often starts before the driver settles. Each field read independently so one missing metric (e.g. power draw isn't reported by every card) doesn't kill the whole GPU reading.

### Spotify

Two providers, one `Reading` shape, selected by required `mode` (no default - accidentally running the mock is worse than refusing to start).

- `mode: "mock"` - configured track, no network.
- `mode: "api"` - real Spotify Web API.

**Auth**: Authorization Code + PKCE, no client secret. Settings: `client_id`, `redirect_uri` (must match the Spotify app), `state_file`.

- `GET /spotify/connect` - starts auth, requires bearer token.
- `GET /spotify/callback` - OAuth redirect target, can't require the token (fresh tab has none). Protected by single-use `state` value, 10 min expiry (RFC 8252 loopback-redirect model).

Both routes are optional interfaces (`RouteProvider`, `OpenRouteProvider`), same pattern as `AssetProvider` for album art.

**Storage**: refresh token in `state_file`, not `config.json` (config is hand-edited, this is agent-written). Atomic write, mode 0600 (NTFS doesn't enforce POSIX perms, so no stronger than config's exposure on Windows).

**Scope**: `user-read-currently-playing`, `user-read-playback-state`, `user-modify-playback-state`. Existing connections need to reconnect for the write scope.

**Polling**: `GET /me/player/currently-playing`, default 5s. Token refreshed before expiry or on 401. No content/non-track = empty reading (success). Not connected/revoked = failure with a `/spotify/connect` hint.

**Art**: fetched once per track, cached next to `state_file`, served from the agent's own origin. Re-fetched only on track ID change. `ArtURL` carries track ID as a query param so the cache/URL guard don't freeze the cover on the first track's art.

**Consistency after a skip**: `currently-playing` doesn't reliably reflect a `next`/`previous` right away (measured: <200ms to >1s). `handleControl` flags `expectingChange`; the next poll retries briefly (`consistencyRetries`, `consistencyDelay`) rather than trusting a stale read. Pause/resume skip this - `Playing` reflects immediately.

**Layout**: `"fill"` or `"disc"`, default `"fill"`, config-only (shell loads one fixed URL, no room for a query param).

### Calendar

Same two-provider shape as Spotify: `mode: "mock"` or `mode: "ics"` (real feed, no OAuth - Outlook and friends publish ICS URLs natively).

Next-up event is primary: title, location (feed's raw `LOCATION`), start time, countdown ticking locally between polls (same as Spotify's position). Below it, a short agenda (`AgendaSize`, 3 total) of what follows - title and start time only. No multi-calendar merge, no editing.

**Parsing** (`ics.go`): minimal hand-rolled RFC 5545 reader, not a library. Reads `SUMMARY`, `LOCATION`, `DTSTART` (UTC, named `TZID`, or floating local), `RRULE`. `time/tzdata` embedded so `TZID` resolves without a host timezone DB. All-day events excluded from next-up (a countdown means nothing for one).

**Recurrence** (`rrule.go`): the RFC 5545 shapes an actual calendar uses, not the full spec - `FREQ` daily/weekly/monthly/yearly, `INTERVAL`, `COUNT`, `UNTIL`, `BYDAY` (plain weekday, weekly only), `BYMONTHDAY` (positive, monthly only). `nextOccurrence` walks forward from `DTSTART` (capped at 500 occurrences) to the first hit after now.

Unsupported: ordinal `BYDAY` ("3rd Thursday"), negative `BYMONTHDAY`, `BYSETPOS`, `BYWEEKNO`, `BYYEARDAY`, `WKST`, sub-daily frequencies. `parseRRule` reports these; `nextUpEvent` excludes events it can't expand.

**Auto-switch**: source decides urgency, not the client. Reading carries `urgent` (within `notify_minutes`, default 15) and `show_seconds` (default 45). Client switches to the calendar face on a new urgent event, overrides sleep window for the duration, holds `show_seconds`, then returns - unless the viewer already tapped away.

### State and transport

State store holds latest value per source (timestamp + status) - single source of truth for both `/health` and `/ws`.

| Endpoint       | Auth  | Behaviour                                                        |
| -------------- | ----- | ---------------------------------------------------------------- |
| `/health`      | none  | JSON: uptime, version, per-source status, last update, last error. |
| `/ws`          | token | Full snapshot on connect, then one message per source update.     |
| `/` and static | token | Embedded web UI (`embed.FS`).                                     |

### How a browser authenticates

Bearer header works for a programmatic client but not a browser (can't set headers on navigation or asset loads). So the token is accepted three ways, in order:

1. `Authorization: Bearer <token>` - programmatic.
2. `?token=<token>` - opening the panel URL once.
3. `spotdash_session` cookie, set by (2) on success.

Cookie is `HttpOnly`, `SameSite=Strict`, no `Expires`/`Max-Age` (session only, never disk). Header auth gets no cookie. `SameSite=Strict` + no state-changing endpoints stands in for CSRF protection.

Token appears once in a URL (proxy/access-log exposure risk, accepted for phase 1); page strips it from the address bar immediately.

WebSocket message shape:

```json
{ "source": "telemetry", "ts": "2026-09-14T10:04:11Z", "data": {} }
```

Connect snapshot is a sequence of the same message shape. A source that's never polled is left out (no blank-reading render). Only successful polls broadcast; a failure just changes status via `/health`, last good reading stays.

### Backpressure

Store fans out without blocking. Each subscriber gets a small buffer; one that fills it is dropped (channel closed) rather than silently skipped - reconnect gets a fresh snapshot instead of quietly stale data. Keeps one wedged panel from stalling every source.

### Liveness

Panel only listens, so the server never reads from the connection - meaning close isn't acknowledged and a vanished client isn't noticed, without help. Handler drains/discards incoming frames (fast close ack + peer-gone detection) and pings every 30s to catch silent drops (wifi kiosk disappearing without closing).

`/health` unauthenticated on purpose - status/errors only, never data or the token.

## Web UI

Plain HTML/CSS/vanilla JS, no framework, no build step (target is a WebView on a 2017 MediaTek SoC).

### Faces

```js
export function render(container, state) {}
export function onState(source, data) {}
```

`render` builds DOM once, `onState` mutates it. One face shown at a time, tap left/right half to switch, `?face=` on load. A throwing face is contained, not fatal.

Tap order: `clock`, `calendar`, `spotify`, `telemetry`, `status`. `clock` first (shown most), `status` last (debug face: every source's status/last update/last error, agent uptime, socket state - fallback when the socket is down).

`clock_style` (`"digital"`/`"analogue"`) picks how `clock` draws (text + seconds arc vs hands). `hide_next_event` (bool, default false) independently hides the next calendar line. Digital: next-up sits below the date. Analogue: no room on the rim for a second line, so it moves inward between hub and numeral ring, drawn over the hands (they sweep behind it). Hand math (`handAngles` in `clock.js`) is pure and unit tested apart from the DOM.

**CSS gotcha**: `.face` centres via `transform: translate(-50%, -50%)`, which creates a stacking context - any descendant `z-index` is trapped inside it and can never outrank a sibling like `.zone` (the tap zones). `.spotify` and `.calendar` override with `transform: none` for this reason; without it, scroll gestures on `.calendar-agenda` get swallowed by `.zone`.

### The rim

Every face draws quantity on a circular track, detail in the centre - clock sweeps seconds, status splits into per-source segments, telemetry hangs four gauges on the quarters. Load-bearing, not decorative: health/progress/load readable across the room. New faces get the language for free.

### Colour

Resting = cool (`--live`, blue), alerts = warm (amber >80%, red >95%, or >83C GPU temp). `--live` is the one accent token everything derives from - configurable via `accent_color`, applies live within one `/health` poll.

Missing reading is a third state (not zero) - absent GPU renders as absent with a reason, not a calm empty gauge or a false alarm.

### Settings

`GET/POST /settings`, same auth as everything except `/health` and OAuth callbacks. GET reports `accent_color`, `hidden_faces`, `clock_style`, `hide_next_event`; POST validates all four together, writes via `config.Save` (atomic), reloads. `clock` can never appear in `hidden_faces`.

Settings page groups `clock_style`/`hide_next_event` under "Clock" ahead of the generic "Faces" list; `clock` has no row there since it can't be hidden.

Reload is scheduled via `time.AfterFunc` shortly after the response is sent, not called inline - calling it inside the POST handler would deadlock (`Shutdown` waits on the handler, handler waits on `Reload`).

All four settings ride `/health` and apply live client-side, guarded to a no-op when nothing changed (this runs every health poll; rebuilding the current face's DOM needlessly would reset scroll position etc).

### Layout

480x480 square, circular clip-path, dark background. Large high-contrast type sized for ~60cm viewing. Corners are invisible on the real device - content stays inside the inscribed circle.

### Token handling in the UI

Token arrives once as `?token=`, stripped from the URL via `replaceState`, held in memory only (never localStorage/sessionStorage). Only used afterward for the WebSocket handshake, as a subprotocol value (browsers can't set WS headers; a query param would get logged).

### Where status comes from

Socket carries readings. Status/uptime/last-error come from `/health`, polled every 5s - unauthenticated, keeps answering when the socket is down, which is exactly when the status face needs to be truthful.

### Reconnection

WebSocket client reconnects with exponential backoff + jitter on close/error. A connection can go silently stale without either firing (WebView backgrounding is the leading suspect) - a watchdog closes the connection if 60s pass with nothing heard while it still thinks it's live, feeding into the same close handler as a real disconnect.

## Shell

Single Activity, minSdk/targetSdk 30. Fullscreen immersive, screen on, no bars. Declares `HOME`/`DEFAULT` intents so LineageOS can set it as default launcher.

Agent URL and token live in `EncryptedSharedPreferences`, entered via a 3s long-press settings screen (the only UI besides the WebView - no other input on the device). Token injected as a query param on initial load only; the page holds it afterward.

### JavaScript bridge

`window.shell` exposes four methods:

| Method                 | Purpose                        |
| ---------------------- | ------------------------------ |
| `setBrightness(0-255)` | Panel brightness.              |
| `screenOff()`          | Blank the panel.               |
| `screenOn()`           | Wake the panel.                |
| `keepAwake(bool)`      | Hold or release the wake lock. |

Each no-ops (and logs why) when the permission is missing - keeps the UI working unchanged in an emulator.

### Failure behaviour

WebView load failure, an HTTP error on the panel page (a 401 or 403 says the token was rejected), or agent unreachable 30s+, shows a native fallback (agent URL without the token, the error, how to open settings) and retries with a growing delay, 5s doubling to 60s. Native because the web layer is what's in question. Shell polls `/health` itself rather than asking the page, so the fallback still works if the WebView itself is broken.

30s delay is deliberate - a restarting agent is usually back in a second or two, and flashing a fallback on every restart would be worse than a briefly stale dashboard.

### Scaling

480 CSS px fixed layout; shell computes initial scale from real display width so it fits whatever density it lands on (Spot's density need not match the emulator's).

## Security posture for phase 1

LAN-bound, `Authorization: Bearer <token>` on every endpoint except `/health`, no TLS. Anyone with LAN access observing traffic can read the token and dashboard data - accepted for phase 1 (desktop telemetry + clock, home LAN, fixed kiosk). Revisit if: a second user, off-LAN access, or a sensitive source gets added.

In scope now: required non-empty token, `/health` never leaks data/token, token never persisted client-side, `config.json` gitignored.

Out of scope: TLS, per-client credentials, token rotation, rate limiting, any write path from UI to agent. No command endpoints, so a leaked token only reads.

## Lifecycle

Start/reload/stop live in `internal/app` (tray excluded - needs a desktop session a test doesn't have). Every tray item is one call into it; `-no-tray` runs the same agent as a console process.

Reload replaces the whole running config (listen/token/sources can all change) - fail closed like startup, and then some: new config loads and builds sources before touching anything running, so a bad config leaves the agent untouched. If the new config validates but can't be served (e.g. port taken), the previous config is restored.

Shutdown: stop accepting requests, close connections, stop sources, wait for their goroutines. Ctrl+C and tray Quit converge here; a signal also kills the tray.

## Logging

Structured, to stderr and a rotating file next to the binary. Every source poll logged at debug with outcome + duration.

## Non-goals for phase 1

No Spotify, calendar, weather, voice, or command handling. No TLS. No installer, no Windows service. No animation beyond simple transitions.
