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

`config.json` is read at startup, on the tray's Reload config, and when the settings page saves. Any other edit needs one of those or a restart.

`agent/config.example.json` has a block for every source. `calendar` and `spotify` are switched off with placeholder values so a fresh copy runs clean. A test in `internal/sources` fails if a source is missing from it, and builds every block switched on so the example cannot drift into keys the source rejects.

Validation is strict and total: missing file, invalid JSON, unknown top-level key, empty or placeholder token, bad `listen`, unknown `log_level`, or non-positive `interval_ms` on an enabled source all exit non-zero naming the key. Unknown top-level keys are rejected (a typo shouldn't silently no-op); keys inside a source block pass through untouched so a source can add settings without touching config.

Log file lives next to the resolved config file.

The agent looks for `config.json` in this order: the `-config` flag, beside the executable, in the working directory, then a `spotdash` folder under the user config directory (`%APPDATA%` on Windows). With no `-config` and no file anywhere, a first run creates the per-user one from `config.example.json` with a generated 32 character token, mode 0700 on the folder. The file is written under a temporary name and hard linked into place. The link fails if the target exists, so two first runs cannot overwrite each other and a crash leaves no truncated file. A config that exists but fails to load is never replaced. A `-config` path is never created.

Relative file paths in a source (spotify `state_file`, mock `art_file`) resolve against the directory `config.json` is in, never the working directory, which the agent does not control when it starts at login. `config.Load` hands each source that directory as `Source.Dir`; it is not written back by `Save`. Absolute paths are used as written.

### Source settings reference

Every source block takes `enabled` (bool) and `interval_ms` (int, required > 0 when enabled), plus whatever's below.

**`clock`** - no required keys.

| Key           | Type   | Default | Notes                                                         |
| ------------- | ------ | ------- | -------------------------------------------------------------- |
| `sleep_start` | string | none    | `"HH:MM"`. Must be set together with `sleep_end` or not at all. |
| `sleep_end`   | string | none    | `"HH:MM"`. No sleep window (panel never blanks) if both are absent. |

**`telemetry`** - no settings beyond `enabled`/`interval_ms`. `gpu` in the reading is `null` when NVML is unavailable.

**`spotify`** - `mode` is required, no default.

| Key             | Type   | Modes  | Notes                                                              |
| --------------- | ------ | ------ | ------------------------------------------------------------------- |
| `mode`          | string | both   | `"mock"` or `"api"`.                                                 |
| `layout`        | string | both   | `"fill"` or `"disc"`, default `"fill"`.                              |
| `track`         | string | mock   | Required with `mode: "mock"`.                                       |
| `artist`        | string | mock   | Optional.                                                            |
| `album`         | string | mock   | Optional.                                                            |
| `duration_ms`   | int    | mock   | Required with `mode: "mock"`, must be positive. Track length in ms. |
| `art_file`      | string | mock   | Optional path to a local image, served as the mock's album art. Relative to `config.json`'s directory. |
| `paused`        | bool   | mock   | Optional, default `false`.                                          |
| `client_id`     | string | api    | Required. Spotify app's Client ID (no secret, PKCE).                 |
| `redirect_uri`  | string | api    | Required. Must exactly match the Spotify app's configured redirect. |
| `state_file`    | string | api    | Required. Where the refresh token is persisted (not `config.json`). The cover art cache sits next to it. A relative path is relative to the directory `config.json` is in, not the working directory. |

**`calendar`** - `mode` is required, no default.

| Key                | Type   | Modes | Notes                                                         |
| ------------------ | ------ | ----- | -------------------------------------------------------------- |
| `mode`             | string | both  | `"mock"` or `"ics"`.                                            |
| `notify_minutes`   | int    | both  | Default 15. Event counts "urgent" within this many minutes.    |
| `show_seconds`     | int    | both  | Default 45. How long an auto-switch holds the face open.        |
| `title`            | string | mock  | Set together with `start_in_minutes` or `start_at`; leave all three unset for no event. |
| `location`         | string | mock  | Optional.                                                       |
| `start_in_minutes` | int    | mock  | Non-zero. Countdown from agent start. Exactly one of this or `start_at`.  |
| `start_at`         | string | mock  | `"HH:MM"`, rolls to tomorrow if already past today.              |
| `upcoming`         | array  | mock  | Canned `{title, start_label}` entries, shown as given.           |
| `feed_url`         | string | ics   | Required. A published ICS URL - treat it as a secret, see "Calendar" below. |

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

A source can implement optional extras beyond the interface. `AssetProvider` serves files such as album art, and `RouteProvider` adds authenticated HTTP routes. `OpenRouteProvider` adds routes that skip the bearer token because the caller cannot carry one (an OAuth callback), so the source must protect the route itself. `RepollRegistrar` receives a function that requests an immediate re-poll, since a source has no reference to the runner. A re-poll restarts the interval from that moment, and the request channel holds one pending request, so a burst of taps neither queues polls nor bunches them closer than the configured interval.

A source named in config with no implementation is a startup error, even disabled - a typo is the likeliest way a working source gets silently switched off. Sources are constructed before the listener opens. A bad one stops the agent at startup, so it cannot sit degraded forever.

### Source status

| Status     | Meaning                                                    |
| ---------- | ---------------------------------------------------------- |
| `ok`       | Last poll succeeded.                                       |
| `degraded` | Enabled but last poll failed, or running with reduced capability (e.g. telemetry without NVML). |
| `disabled` | Off in config. Not polled, not broadcast.                  |

### A third outcome: partial results

Telemetry can be genuinely fine on CPU/RAM/disk but missing GPU. Forcing that into pure success/failure loses something, so a source may return a value plus a partial-marked error: stored and broadcast, source marked `degraded`, not counted toward backoff (nothing's actually failing, and polling less won't fix a missing GPU). Marker lives in its own package to avoid the registry import cycle. A partial error with no value is treated as an ordinary failure, since there is nothing to publish.

### GPU telemetry

NVML (`nvml.dll`, same lib `nvidia-smi` uses) is the only way to read utilisation/VRAM/temp/power. NVIDIA's own `go-nvml` won't build on Windows (uses `dlfcn.h`, POSIX-only, no build tags), so `nvml.dll` is bound directly via `windows.NewLazySystemDLL`, which only resolves from the system directory. Pure Go, no cgo needed for the shipped binary (a C toolchain is still needed for `go test -race`).

Loaded lazily, init retried on every read that finds it unestablished - the agent often starts before the driver settles. Each field read independently so one missing metric (e.g. power draw isn't reported by every card) doesn't kill the whole GPU reading. If the device handle lookup fails, init state is dropped so the next poll starts again, because a driver restart invalidates earlier handles. Return codes are named from a fixed table, since reading `nvmlErrorString` needs a pointer into memory Go doesn't own and `go vet` rejects it.

### Spotify

Two providers, one `Reading` shape, selected by required `mode` (no default - accidentally running the mock is worse than refusing to start).

- `mode: "mock"` - configured track, no network.
- `mode: "api"` - real Spotify Web API.

Auth: Authorization Code + PKCE, no client secret. Settings: `client_id`, `redirect_uri` (must match the Spotify app), `state_file`.

- `GET /spotify/connect` - starts auth, requires bearer token.
- `GET /spotify/callback` - OAuth redirect target, can't require the token (fresh tab has none). Protected by single-use `state` value, 10 min expiry (RFC 8252 loopback-redirect model).

Both routes are optional interfaces (`RouteProvider`, `OpenRouteProvider`), same pattern as `AssetProvider` for album art.

Storage: refresh token in `state_file`, never `config.json` (config is hand-edited, this is agent-written). Atomic write, mode 0600 (NTFS doesn't enforce POSIX perms, so no stronger than config's exposure on Windows).

Scope: `user-read-currently-playing`, `user-read-playback-state`, `user-modify-playback-state`. Existing connections need to reconnect for the write scope. The write scope also allows volume, seek, shuffle, repeat, device transfer and queueing. The agent exposes only pause, resume, next and previous, because those are the only methods on the `playbackController` interface.

Polling: `GET /me/player/currently-playing`, default 5s. Token refreshed before expiry or on 401. No content/non-track = empty reading (success). Not connected/revoked = failure with a `/spotify/connect` hint.

Art: fetched once per track, cached next to `state_file`, served from the agent's own origin. Re-fetched only on track ID change. `ArtURL` carries track ID as a query param so the cache/URL guard don't freeze the cover on the first track's art.

Consistency after a skip: `currently-playing` doesn't reliably reflect a `next`/`previous` right away (measured: <200ms to >1s). `handleControl` flags `expectingChange`; the next poll does not trust a stale read and retries briefly (`consistencyRetries`, `consistencyDelay`). Pause/resume skip this - `Playing` reflects immediately.

Layout: `"fill"` or `"disc"`, default `"fill"`, config-only (shell loads one fixed URL, no room for a query param).

### Calendar

Same two-provider shape as Spotify: `mode: "mock"` or `mode: "ics"` (real feed, no OAuth - Outlook and friends publish ICS URLs natively).

Next-up event is primary: title, location (feed's raw `LOCATION`), start time, countdown ticking locally between polls (same as Spotify's position). Below it, a short agenda (`AgendaSize`, 3 total) of what follows - title and start time only. No multi-calendar merge, no editing.

Parsing (`ics.go`): minimal hand-rolled RFC 5545 reader with no library. Reads `SUMMARY`, `LOCATION`, `DTSTART` (UTC, named `TZID`, or floating local), `RRULE`. `time/tzdata` embedded so `TZID` resolves without a host timezone DB. All-day events excluded from next-up (a countdown means nothing for one).

Recurrence (`rrule.go`): the RFC 5545 shapes an actual calendar uses, a subset of the full spec - `FREQ` daily/weekly/monthly/yearly, `INTERVAL`, `COUNT`, `UNTIL`, `BYDAY` (plain weekday, weekly only), `BYMONTHDAY` (positive, monthly only). `nextOccurrence` walks forward from `DTSTART` (capped at 500 occurrences) to the first hit after now.

Unsupported: ordinal `BYDAY` ("3rd Thursday"), negative `BYMONTHDAY`, `BYSETPOS`, `BYWEEKNO`, `BYYEARDAY`, `WKST`, sub-daily frequencies. `parseRRule` reports these; `nextUpEvent` excludes events it can't expand. Each event adds one agenda entry, its next occurrence, so a daily standup can't crowd out the rest.

Auto-switch: source decides urgency, not the client. Reading carries `urgent` (within `notify_minutes`, default 15) and `show_seconds` (default 45). Client switches to the calendar face on a new urgent event, overrides sleep window for the duration, holds `show_seconds`, then returns - unless the viewer already tapped away.

### State and transport

State store holds latest value per source (timestamp + status) - single source of truth for both `/health` and `/ws`.

| Endpoint       | Auth  | Behaviour                                                        |
| -------------- | ----- | ---------------------------------------------------------------- |
| `/health`      | none  | JSON: agent time (`now`, UTC), uptime, version, protocol version, per-source status, last update, last error. |
| `/ws`          | token | Hello frame, then a full snapshot on connect, then one message per source update. |
| `/` and static | token | Embedded web UI (`embed.FS`).                                     |

`/health` also carries `accent_color`, `hidden_faces`, `clock_style` and `hide_next_event`. They are cosmetic, and the panel needs them before it has anything else confirming the agent is reachable. `now` is the agent's clock in UTC. The device has no battery-backed RTC, so the panel corrects its own clock against it when showing how old a reading is.

Content types for the embedded UI and served files come from a fixed table. `mime.TypeByExtension` reads the Windows registry, where `.js` is often `text/plain`, and browsers refuse a module served that way. Responses are `no-store` because the binary is rebuilt often and the device caches hard. Album art is read from disk on each request, since the file changes with the track.

### How a browser authenticates

Bearer header works for a programmatic client but not a browser (can't set headers on navigation or asset loads). So the token is accepted three ways, in order:

1. `Authorization: Bearer <token>` - programmatic.
2. `?token=<token>` - opening the panel URL once.
3. `spotdash_session` cookie, set by (2) on success.

Cookie is `HttpOnly`, `SameSite=Strict`, no `Expires`/`Max-Age` (session only, never disk). Header auth gets no cookie. `SameSite=Strict` + no state-changing endpoints stands in for CSRF protection.

Token appears once in a URL (proxy/access-log exposure risk, accepted for phase 1); page strips it from the address bar immediately.

There is no loopback exemption. A request from the same machine needs the token like any other, and the tray's Open UI URL works because the agent attaches its own token. Unknown paths answer 401 instead of 404, so an unauthenticated caller cannot map the routes.

The WebSocket authorises itself before the upgrade, outside the token middleware, because its token arrives as a `bearer.<token>` subprotocol value. It also accepts the session cookie and the bearer header, in that order.

A source can register an open route for a caller that cannot carry the token, such as an OAuth redirect into a fresh tab. Open routes live on a separate mux, so registering one cannot expose another pattern, and the handler has to protect itself.

WebSocket message shape:

```json
{ "source": "telemetry", "ts": "2026-09-14T10:04:11Z", "data": {} }
```

Connect snapshot is a sequence of the same message shape. A source that's never polled is left out (no blank-reading render). Only successful polls broadcast; a failure just changes status via `/health`, last good reading stays.

The first frame on every connection is the hello, `{"source":"protocol","ts":"...","data":{"protocol":1,"agent":"v0.1.0"}}`. It is written straight to the socket and never enters the store, so it is not a snapshot entry, not in `/health` `sources`, and the panel keeps it out of its source list.

### Protocol version

The agent and the shell ship as separate downloads, so they can drift. `ProtocolVersion` (`agent/internal/server/protocol.go`) is one integer for what they must agree on: the JavaScript bridge methods and the provisioning payload. The shell has its own copy, `PROTOCOL_VERSION` (`shell/app/src/main/java/dev/spotdash/shell/Protocol.kt`). It starts at 1. Bump the side that changed, and only for an incompatible change: a removed or renamed bridge method, a changed method contract, or a provisioning payload the other side would reject. Adding a method or an optional field does not need a bump. The panel is embedded in the agent, so panel and agent cannot drift.

Each side announces it:

- The agent reports `protocol` on `/health` and in the hello frame.
- The shell appends `spotdash-shell/<versionName> proto/<n>` to the WebView user agent. This adds no bridge method.

Each side checks the other:

- The agent reads the token from the user agent on every WebSocket handshake and logs one warning naming both versions when `n` differs. A browser has no token and is not checked.
- The panel compares the shell token in `navigator.userAgent` with the agent's protocol and shows "Update the shell" when the shell is lower, "Update the agent" when it is higher. No token or an equal value shows nothing.
- The shell reads `protocol` from every `/health` answer. On a difference it raises the native fallback with the same wording, which works when the web layer does not. It clears once the values agree. An agent that reports no `protocol` is an older build and is not treated as a mismatch.

The status face footer shows the agent and shell versions. The tray tooltip is `spotdash <version>`.

### Backpressure

Store fans out without blocking. Each subscriber gets a small buffer. One that fills it is dropped (channel closed) and reconnects to a fresh snapshot. Silently skipping messages would leave it quietly stale. Keeps one wedged panel from stalling every source.

The buffer holds 32 messages, enough for a garbage collection pause or a frame the device spent elsewhere. A client that has stopped reading is dropped.

### Liveness

Panel only listens, so the server never reads from the connection - meaning close isn't acknowledged and a vanished client isn't noticed, without help. Handler drains/discards incoming frames (fast close ack + peer-gone detection) and pings every 30s to catch silent drops (wifi kiosk disappearing without closing). Without the ping, a clock source writing every second would be the only liveness signal, and a write to a dead socket can sit buffered for a long time.

`/health` is unauthenticated. It carries status and errors only, never data or the token.

## Web UI

Plain HTML/CSS/vanilla JS, no framework, no build step (target is a WebView on a 2017 MediaTek SoC).

### Faces

```js
export function render(container, state) {}
export function onState(source, data) {}
```

`render` builds DOM once, `onState` mutates it. One face shown at a time, tap left/right half to switch, `?face=` on load. A throwing face is contained and never fatal.

Tap order: `clock`, `calendar`, `spotify`, `telemetry`, `status`. `clock` first (shown most), `status` last (debug face: every source's status/last update/last error, agent uptime, socket state - fallback when the socket is down).

`clock_style` (`"digital"`/`"analogue"`) picks how `clock` draws (text + seconds arc vs hands). `hide_next_event` (bool, default false) independently hides the next calendar line. Digital: next-up sits below the date. Analogue: no room on the rim for a second line, so it moves inward between hub and numeral ring, drawn over the hands (they sweep behind it). Hand math (`handAngles` in `clock.js`) is pure and unit tested apart from the DOM.

CSS gotcha: `.face` centres via `transform: translate(-50%, -50%)`, which creates a stacking context - any descendant `z-index` is trapped inside it and can never outrank a sibling like `.zone` (the tap zones). `.spotify` and `.calendar` override with `transform: none` for this reason; without it, scroll gestures on `.calendar-agenda` get swallowed by `.zone`.

`spotify` has two layouts. The default lets the cover fill the panel, which has the most presence but depends on the sleeve being dark where the type sits. `?layout=disc` keeps the type on flat black, for a bright or busy sleeve. The agent's config also sets the layout, because the real device loads one fixed URL and cannot carry a query parameter; a config value wins on every reading and an absent one leaves the query default alone. The transport buttons need a stacking order above `.zone` for the same reason as the CSS gotcha above, and a tap applies its known outcome to the icon at once without waiting for the next reading.

### The rim

Every face draws quantity on a circular track, detail in the centre - clock sweeps seconds, status splits into per-source segments, telemetry hangs four gauges on the quarters. It carries meaning: health/progress/load readable across the room. New faces get the language for free.

On `calendar` the rim carries urgency. It fills over a 60 minute lookahead window, so an event further out shows an empty rim, and it steps from live to warn at 15 minutes and to alert at 5, matching the telemetry vocabulary. Those thresholds are only a visual cue and are separate from the source's auto-switch.

### Colour

Resting = cool (`--live`, blue), alerts = warm (amber >80%, red >95%, or >83C GPU temp). `--live` is the one accent token everything derives from - configurable via `accent_color`, applies live within one `/health` poll.

Missing reading is a third state, distinct from zero. An absent GPU renders as absent with a reason, never as a calm empty gauge or a false alarm.

### Settings

`GET/POST /settings`, same auth as everything except `/health` and OAuth callbacks. GET reports `accent_color`, `hidden_faces`, `clock_style`, `hide_next_event`; POST validates all four together, writes via `config.Save` (atomic), reloads. `clock` can never appear in `hidden_faces`.

Settings page groups `clock_style`/`hide_next_event` under "Clock" ahead of the generic "Faces" list; `clock` has no row there since it can't be hidden.

Reload is scheduled via `time.AfterFunc` shortly after the response is sent. Calling it inside the POST handler would deadlock (`Shutdown` waits on the handler, handler waits on `Reload`).

All four settings ride `/health` and apply live client-side, guarded to a no-op when nothing changed (this runs every health poll; rebuilding the current face's DOM needlessly would reset scroll position etc).

### Layout

480x480 square, circular clip-path, dark background. Large high-contrast type sized for ~60cm viewing. Corners are invisible on the real device - content stays inside the inscribed circle.

The target WebView (LineageOS 18.1) is around Chromium 83. It has no `inset` shorthand, no flex `gap` (added in 84, silently ignored before it, so items touch), and no `:focus-visible`. The stylesheet uses explicit `top/right/bottom/left` offsets and sibling margins instead. `.zone:focus` has its outline removed because a tap leaves the zone focused and the circular clip turns the WebView's ring into a stray vertical line; `:focus-visible` restores a ring for keyboard use on desktop, and Chromium 83 ignores that rule.

### Token handling in the UI

Token arrives once as `?token=`, stripped from the URL via `replaceState`, held in memory only (never localStorage/sessionStorage). Only used afterward for the WebSocket handshake, as a subprotocol value (browsers can't set WS headers; a query param would get logged).

### Where status comes from

Socket carries readings. Status/uptime/last-error come from `/health`, polled every 5s - unauthenticated, keeps answering when the socket is down, which is exactly when the status face needs to be truthful.

`/health` also carries the agent's clock. The panel keeps the difference from the device clock as `clockOffsetMs` and subtracts it before showing an age, so a skewed device clock does not make healthy sources look stale. The offset includes the response's travel time, which is milliseconds on a LAN and far below the whole seconds ages are shown in. It stays zero until the agent has reported a time.

### Reconnection

WebSocket client reconnects with exponential backoff + jitter on close/error. A connection can go silently stale without either firing (WebView backgrounding is the leading suspect) - a watchdog closes the connection if 60s pass with nothing heard while it still thinks it's live, feeding into the same close handler as a real disconnect.

The watchdog checks every 10 seconds, well under its 60 second threshold, so it gets several chances before the threshold passes. It acts only while the panel believes it is live, since a connection already in backoff has no meaningful last-heard time, and it never fires for a panel that has not yet connected.

## Shell

Single Activity, minSdk/targetSdk 30. Fullscreen immersive, screen on, no bars. Declares `HOME`/`DEFAULT` intents so LineageOS can set it as default launcher.

Agent URL and token live in `EncryptedSharedPreferences`, entered via a 3s long-press settings screen (the only UI besides the WebView - no other input on the device). That screen also has a "Wi-Fi networks" button that opens Android's own Wi-Fi settings, since the shell is the launcher and there is otherwise no way to reach them without adb. It is a deep link because a screen of our own cannot join a network: on API 30 `WifiManager.addNetwork` is ignored for apps targeting API 29+, and a network request only connects this process. Coming back from it retries the panel at once. Token injected as a query param on initial load only; the page holds it afterward.

The wifi screen is the system one because it already handles WPA2 and WPA3, hidden networks and forgetting a network, which a screen of our own would have to rebuild. The button sits on the settings screen because it works with nothing configured, and a device that cannot reach the agent is when it is needed.

The URL and token are encrypted because the token is a shared secret on a device anyone can pick up. If the keystore is corrupted the shell falls back to plain storage. Otherwise the launcher would crash at boot, and with no other launcher that bricks the device until it is reflashed. The token field on the settings screen is visible, since a masked field is hard to type accurately on the 480px circle. The long press takes three seconds so dusting the screen does not open settings. The touch listener never consumes the event, so the page still gets its own taps. The back button does nothing. The WebView cache is off because the agent is rebuilt often and a stale panel with no address bar is hard to diagnose.

### Provisioning from adb

Typing a URL and a long token on the 480px circle is the worst step of setup, so the shell can also take both from a file pushed with adb (`Provisioning.kt`, applied from `PanelActivity`). The long-press settings screen stays as the manual route.

Three ways to hand the shell a secret without typing it were compared:

| | Where it goes | What is wrong with it |
| --- | --- | --- |
| File in the app's own external files directory | `Android/data/dev.spotdash.shell/files/provision.json`, read once, then deleted | Needs root adb on API 30 (below). The token sits on storage until consumed, so whatever pushes it must remove it on failure |
| Intent extras on the launcher | `adb shell am start --es ...` | The launcher activity has to stay `exported`, so any other app on the device can send the same intent and re-point the panel. The shell appends the token to whatever URL it holds, so that hands the token to the sender's host. Only tolerable if limited to the unconfigured state |
| File in `/data/local/tmp` | world-readable scratch space | Rejected: readable by every app while it sits there, and the app cannot delete it |

Threat model, for a single-purpose device with SELinux permissive and adb already implying full control: the exposures that matter are a token left lying on shared storage, another app re-pointing the panel, and half-applied or malformed input. The file route adds no exported surface, so it has none of the second. Chosen: the file.

Rules, all fail closed:

- One flat JSON object, `{"version":1,"agent_url":"...","token":"..."}`. A repeated or unknown key, a wrong type, a wrong version, a missing field, trailing text or a truncated file rejects the whole payload. Read by a small strict parser, since `org.json` is stubbed out in JVM unit tests.
- The address must be `http` or `https` with a host, no credentials, a port of 1 to 65535 if any, and no path, query or fragment. The token must be non-empty printable ASCII, at most 256 characters, no whitespace. There is no minimum length: the agent decides what a token has to be.
- The address and token are stored in one commit, or not at all. A rejected payload changes nothing and logs `provisioning ignored: <reason>`. The reason never contains a value from the payload, so the token is never in logcat. A good one logs `provisioning applied for <host>`.
- The file is deleted in every case, valid or not, and at most 4 KiB is read. A leading byte order mark is tolerated, since Windows PowerShell 5.1 writes one.
- The file is looked at when the panel comes to the front and again from `onNewIntent`, because `am start` on an activity that is already in front only delivers an intent and does not pause and resume it.

The address and token are written with a single `commit()` because the alternatives are both bad. A new address with the old token gives a panel that loads and is rejected. A new token with the old address sends a secret to the wrong host. The settings screen still writes the two fields separately, since a person edits one box at a time.

Verified on an API 30 emulator (userdebug, SELinux enforcing), not yet on the Spot. There, the shell user cannot write to `Android/data/<package>` at all, since the directory belongs to the app and the group `ext_data_rw`, which `shell` is not in. A root push through the normal `/sdcard` path lands with the wrong security label (`storage_file`) and the app gets `EACCES`. What works is `adb root` and pushing straight to the underlying path `/data/media/0/Android/data/dev.spotdash.shell/files/`, where the file gets the right label and the app can read and delete it. The directory is created when the shell first starts, so start it once before pushing. `docs/device.md` already relies on `adb root` for this ROM (Wi-Fi join, timezone). Not tried: whether `/data/media/0` is the right path on the Spot's ROM.

### JavaScript bridge

`window.shell` exposes four methods:

| Method                 | Purpose                        |
| ---------------------- | ------------------------------ |
| `setBrightness(0-255)` | Panel brightness.              |
| `screenOff()`          | Blank the panel.               |
| `screenOn()`           | Wake the panel.                |
| `keepAwake(bool)`      | Hold or release the wake lock. |

Each no-ops (and logs why) when the permission is missing - keeps the UI working unchanged in an emulator.

Nothing in the bridge throws, so the panel behaves the same on the Echo Spot, which grants some of these permissions, and on an emulator, which grants none. Each method hops to the main thread, because WebView calls from its own JavaScript thread and touching a window from there crashes. `screenOff()` sets the brightness to zero. Powering the display down would need `DEVICE_ADMIN`, a heavier grant that an emulator does not offer.

### Failure behaviour

WebView load failure, an HTTP error on the panel page (a 401 or 403 says the token was rejected), or agent unreachable 30s+, shows a native fallback (agent URL without the token, the error, how to open settings) and retries with a growing delay, 5s doubling to 60s. Native because the web layer is what's in question. Shell polls `/health` itself, so the fallback still works if the WebView itself is broken.

The 30s delay is there because a restarting agent is usually back in a second or two, and flashing a fallback on every restart would be worse than a briefly stale dashboard.

WebView also calls `onPageFinished` after a failed load, and again when a retry abandons a load that was hanging on an unreachable agent. Neither means the panel is showing. A finished page hides the fallback only when the load had no error and the watcher does not consider the agent down. The watcher keeps that down state across a stop and start. It is stopped whenever the panel pauses, for example while the Wi-Fi settings are open, and forgetting the outage would let a hung load clear the fallback. Recovery is still reported, because the first good probe calls the up callback.

An unconfigured shell and a rejected token get their own fallback titles, since neither is an unreachable agent. The retry delay starts at 5s because the usual cause is an agent about to come back. It stops at 60s because the other cause is a token nobody has fixed, and retrying faster does not help. It resets after a successful load, a settings change or a recovery. `/health` needs no token, which is why it still works when everything else is broken.

### Debugging the panel

The panel's `console.*` output goes to logcat under the `spotdash` tag (`adb logcat -s spotdash`), at the matching level. A face that throws is drawn on screen and also logged with `console.error`. `?token=` and `bearer.` values are redacted, since anyone with adb can read logcat.

A debuggable build also turns on WebView remote debugging, so the panel shows up under chrome://inspect/#devices. A release build doesn't ask for it, but Chromium's WebView enables it by itself on a `userdebug` system image, which is what LineageOS builds usually are, so don't rely on it being off there.

### Scaling

480 CSS px fixed layout; shell computes initial scale from real display width so it fits whatever density it lands on (Spot's density need not match the emulator's).

The display is 240dpi, so without scaling the 480 CSS px layout would render at 720 physical pixels and overflow the glass. The shell turns on wide viewport handling and sets the initial scale to the display width over 480, clamped to 25 to 400 percent, and logs the result.

## Security posture for phase 1

LAN-bound, `Authorization: Bearer <token>` on every endpoint except `/health`, no TLS. Anyone with LAN access observing traffic can read the token and dashboard data - accepted for phase 1 (desktop telemetry + clock, home LAN, fixed kiosk). Revisit if: a second user, off-LAN access, or a sensitive source gets added.

In scope now: required non-empty token, `/health` never leaks data/token, token never persisted client-side, `config.json` gitignored.

Out of scope: TLS, per-client credentials, token rotation, rate limiting, any write path from UI to agent. No command endpoints, so a leaked token only reads.

## Lifecycle

Start/reload/stop live in `internal/app` (tray excluded - needs a desktop session a test doesn't have). Every tray item is one call into it; `-no-tray` runs the same agent as a console process.

Reload replaces the whole running config (listen/token/sources can all change) - fail closed like startup, and then some: new config loads and builds sources before touching anything running, so a bad config leaves the agent untouched. If the new config validates but can't be served (e.g. port taken), the previous config is restored.

Shutdown: stop accepting requests, close connections, stop sources, wait for their goroutines. Ctrl+C and tray Quit converge here; a signal also kills the tray.

Saving from the settings page writes `config.json` and answers first, then reloads 200 ms later from a timer. Reload replaces the listener the request arrived on. Called inline, its shutdown would wait for the handler to return while the handler waited for the reload, and only the 5 second grace period would break the deadlock, by closing the connection under the response. The save starts from a fresh read of the file, so a manual edit made since startup is kept.

One agent per config: `internal/instance` takes a named mutex keyed on the config path (Local namespace, so per user session). A second copy logs "another spotdash is already running for this config" and exits 0 - zero so a scheduled task set to restart on failure does not respawn it. A development copy with its own config still runs.

Start with Windows is a tray checkbox backed by one value under `HKCUSoftwareMicrosoftWindowsCurrentVersionRun` (`internal/autostart`): the quoted absolute path of the running binary, no admin rights. The tick is read from the registry, not remembered, and refreshed on every click and every few seconds, since systray gives no menu-open event on Windows. It refuses, with the reason in the menu text, when the binary is under the temp directory, which is where `go run` builds. The registry sits behind an interface so the logic is tested without it.

## Logging

Structured, to stderr and a rotating file next to the config. Every source poll logged at debug with outcome + duration.

The release build (`agent/tools/build.ps1`, `-H=windowsgui`) has no console, so its stderr is dead and the file is the only record. Stderr writes are best effort so a dead one cannot starve the file (`io.MultiWriter` stops at the first error), and startup failures - bad JSON, an empty or placeholder token, no config, a busy port - are written to the file before the process exits 1. The tray's "View log" opens it.

## Non-goals for phase 1

No Spotify, calendar, weather, voice, or command handling. No TLS. No installer, no Windows service. No animation beyond simple transitions.
