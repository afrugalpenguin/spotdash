# Merge the overview and clock faces Implementation Plan

> **For the implementer:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Retire the `overview` face and fold its next-up-calendar behaviour into
`clock`, so a single face is controlled by two independent settings
(`clock_style`, new `hide_next_event`) instead of two faces competing for the
same job.

**Architecture:** `clock.js` absorbs `overview.js`'s calendar-tracking state
and rendering. `overview.js` is deleted. `config.KnownFaces` drops
`"overview"`; a new `hide_next_event` bool config field (inverted, like
`hidden_faces`, so its Go zero value is the correct default) threads through
`Options` -> `/health` -> the settings page the same way `clock_style`
already does.

**Tech Stack:** Go 1.25 (agent), plain ES modules + `node --test` (web UI), no
build step.

Full design rationale: `docs/plans/2026-09-15-merge-clock-overview-design.md`.

---

## Task 1: `hide_next_event` config field

**Files:**
- Modify: `agent/internal/config/config.go`
- Test: `agent/internal/config/config_test.go`

**Step 1: Write the failing tests**

Add near `TestLoadDefaultsClockStyleToDigital` in `config_test.go`:

```go
func TestLoadDefaultsHideNextEventToFalse(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HideNextEvent {
		t.Error("HideNextEvent = true, want the default of false")
	}
}

func TestLoadAcceptsHideNextEventTrue(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hide_next_event": true,
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load returned an error for a valid hide_next_event: %v", err)
	}
	if !cfg.HideNextEvent {
		t.Error("HideNextEvent = false, want true")
	}
}
```

**Step 2: Run to verify it fails**

Run: `go test ./internal/config/... -run HideNextEvent -v`
Expected: FAIL - `cfg.HideNextEvent undefined (type *Config has no field or method HideNextEvent)`

**Step 3: Add the field**

In `config.go`, in the `Config` struct (right after the `ClockStyle` field):

```go
	// ClockStyle is "digital" or "analogue", how the clock face draws.
	// Defaults to "digital" when absent. Changed from the settings page.
	ClockStyle string `json:"clock_style,omitempty"`
	// HideNextEvent hides the clock face's next-up calendar line. Inverted
	// (a plain bool defaulting to true cannot be told apart from "absent"
	// on decode, the same reason HiddenFaces is a negative list rather
	// than a positive one), so its Go zero value of false is already the
	// correct default: the next event shows unless this says otherwise.
	// Changed from the settings page.
	HideNextEvent bool              `json:"hide_next_event,omitempty"`
	Sources       map[string]Source `json:"sources"`
```

No change needed in `applyDefaults` or `Validate` - a bare bool has nothing
to default (zero value is already correct) or validate (both `true` and
`false` are legal).

**Step 4: Run to verify it passes**

Run: `go test ./internal/config/... -run HideNextEvent -v`
Expected: PASS (2 tests)

**Step 5: Commit**

```bash
git add agent/internal/config/config.go agent/internal/config/config_test.go
git commit -m "feat(config): add hide_next_event setting"
```

---

## Task 2: Remove `overview` from `KnownFaces`

**Files:**
- Modify: `agent/internal/config/config.go:33`
- Test: `agent/internal/config/config_test.go`

**Step 1: Update `KnownFaces`**

```go
var KnownFaces = []string{"clock", "calendar", "spotify", "telemetry", "status"}
```

**Step 2: Fix tests that assumed `overview` was a legal face name**

Search: `grep -n '"overview"' agent/internal/config/config_test.go`. There
should be none (the earlier `fix/clock-face-not-hideable` work already moved
the hidden-faces tests off `"clock"` onto `"calendar"`/`"telemetry"`, and
none of those used `"overview"`). If any turn up, replace with `"calendar"`
or `"telemetry"`, matching the existing `TestLoadAcceptsValidHiddenFaces`
pattern.

**Step 3: Run the full config test suite**

Run: `go test ./internal/config/... -v`
Expected: PASS, all tests (including `TestLoadRejectsHidingEveryFace`, which
uses `strings.Join(KnownFaces, ...)` and so automatically tracks the new
5-face list)

**Step 4: Commit**

```bash
git add agent/internal/config/config.go
git commit -m "feat(config): retire the overview face"
```

---

## Task 3: Thread `HideNextEvent` through `app.go` and `server.go`

**Files:**
- Modify: `agent/internal/server/server.go`
- Modify: `agent/internal/app/app.go`
- Test: `agent/internal/server/server_test.go`
- Test: `agent/internal/app/app_test.go`

**Step 1: Write the failing server test**

In `server_test.go`, near `TestHealthReportsTheConfiguredClockStyle`:

```go
func TestHealthReportsHideNextEvent(t *testing.T) {
	store := state.New()
	srv := New(Options{
		Token:         testToken,
		Version:       "test-version",
		Started:       time.Now(),
		Store:         store,
		HideNextEvent: true,
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		HideNextEvent bool `json:"hide_next_event"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding /health body: %v\nbody: %s", err, rec.Body.String())
	}
	if !body.HideNextEvent {
		t.Error("hide_next_event = false, want true")
	}
}
```

**Step 2: Run to verify it fails**

Run: `go test ./internal/server/... -run HideNextEvent -v`
Expected: FAIL - `unknown field HideNextEvent in struct literal of type Options`

**Step 3: Add the field to `Options` and the health response**

In `server.go`'s `Options` struct, right after `ClockStyle`:

```go
	// ClockStyle is the configured clock_style, "digital" or "analogue".
	// Same reasoning as AccentColor and HiddenFaces: cosmetic, not
	// sensitive, needed before the panel has confirmed anything else.
	ClockStyle string
	// HideNextEvent is the configured hide_next_event. Same reasoning as
	// ClockStyle.
	HideNextEvent bool
```

In the health response struct (the one with `ClockStyle string
json:"clock_style,omitempty"`):

```go
	ClockStyle    string                  `json:"clock_style,omitempty"`
	HideNextEvent bool                    `json:"hide_next_event,omitempty"`
	Sources       map[string]healthSource `json:"sources"`
```

In `handleHealth`, where the response is built:

```go
		ClockStyle:    s.opts.ClockStyle,
		HideNextEvent: s.opts.HideNextEvent,
		Sources:       map[string]healthSource{},
```

**Step 4: Run to verify it passes**

Run: `go test ./internal/server/... -v`
Expected: PASS

**Step 5: Write the failing app test**

In `app_test.go`, near `TestSettingsPostSavesClockStyle`:

```go
func TestSettingsPostSavesHideNextEvent(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	status, respBody := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"hide_next_event":true}`)
	if status != http.StatusOK {
		t.Fatalf("POST /settings = %d, want 200: %s", status, respBody)
	}

	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json: %v", err)
	}
	if !saved.HideNextEvent {
		t.Error("config.json hide_next_event = false, want true")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(healthBody(t, agent.baseURL()), `"hide_next_event":true`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("/health never reported the new hide_next_event after saving")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

**Step 6: Run to verify it fails**

Run: `go test ./internal/app/... -run HideNextEvent -v`
Expected: FAIL (saved.HideNextEvent stays false - nothing wires it yet)

**Step 7: Wire it through `app.go`**

In the `srv := server.New(server.Options{...})` call (the one already
setting `ClockStyle: cfg.ClockStyle`):

```go
		ClockStyle:    cfg.ClockStyle,
		HideNextEvent: cfg.HideNextEvent,
```

In `settingsBody`:

```go
type settingsBody struct {
	AccentColor   string   `json:"accent_color"`
	HiddenFaces   []string `json:"hidden_faces"`
	ClockStyle    string   `json:"clock_style"`
	HideNextEvent bool     `json:"hide_next_event"`
}
```

In `handleSettings`'s GET branch, where `current` is built:

```go
			current = settingsBody{
				AccentColor:   a.current.cfg.AccentColor,
				HiddenFaces:   a.current.cfg.HiddenFaces,
				ClockStyle:    a.current.cfg.ClockStyle,
				HideNextEvent: a.current.cfg.HideNextEvent,
			}
```

In `saveSettings`:

```go
	cfg.AccentColor = body.AccentColor
	cfg.HiddenFaces = body.HiddenFaces
	cfg.ClockStyle = body.ClockStyle
	cfg.HideNextEvent = body.HideNextEvent
```

**Step 8: Run to verify it passes**

Run: `go test ./internal/app/... -v`
Expected: PASS, full suite

**Step 9: Run everything**

Run: `go build ./... && go test ./...` (from `agent/`)
Expected: PASS, all packages

**Step 10: Commit**

```bash
git add agent/internal/server/server.go agent/internal/server/server_test.go agent/internal/app/app.go agent/internal/app/app_test.go
git commit -m "feat(settings): thread hide_next_event through health and settings"
```

---

## Task 4: Remove the `/faces/overview.js` static-serving test entry

**Files:**
- Modify: `agent/internal/server/static_test.go:39`

**Step 1: Edit the file list**

This currently fails once `overview.js` is deleted in Task 7, so do this now
to keep the suite green throughout, but the actual failure won't surface
until Task 7. Remove `"/faces/overview.js"` from the slice:

```go
	for _, path := range []string{"/app.js", "/style.css", "/faces/clock.js", "/faces/status.js", "/faces/telemetry.js", "/faces/rim.js", "/dev.html", "/settings.html", "/settings.js"} {
```

**Step 2: Run**

Run: `go test ./internal/server/... -v`
Expected: PASS (file still exists at this point, so this is a no-op change
until Task 7 deletes it - just moving the edit earlier avoids a second pass
over this file)

**Step 3: Commit**

```bash
git add agent/internal/server/static_test.go
git commit -m "test(server): stop asserting /faces/overview.js is served"
```

---

## Task 5: Port next-up rendering into `clock.js` (digital mode)

This is a straight relocation of `overview.js`'s calendar-tracking logic
into `clock.js`'s digital branch. No behaviour change for digital mode.

**Files:**
- Modify: `agent/web/faces/clock.js`
- Modify: `agent/web/style.css`

**Step 1: Add the calendar imports and module-level state**

At the top of `clock.js`, add to the existing import:

```js
import { createRim, RIM_CIRCUMFERENCE, RIM_RADIUS, setArc } from "./rim.js";
import { urgency, formatCountdown } from "./calendar.js";
```

Add new module-level state, next to the existing `let` declarations:

```js
let nextEl = null;
let nextTitleEl = null;
let nextCountdownEl = null;

// The calendar reading, kept only so paintNext can recompute the countdown
// on every clock tick (once a second) rather than running a second local
// ticker: the clock source already provides that heartbeat for free.
// Ported from the old overview.js face.
let calendarData = null;
let calendarSyncedAt = 0;
let hideNextEvent = false;
```

**Step 2: Build the digital next-up DOM**

In `render()`, in the `else` (digital) branch, after `face.appendChild(dateEl)`
and before `container.appendChild(face)`:

```js
    nextEl = document.createElement("p");
    nextEl.className = "clock-next";
    nextTitleEl = document.createElement("span");
    nextTitleEl.className = "clock-next-title";
    nextCountdownEl = document.createElement("span");
    nextCountdownEl.className = "clock-next-countdown";
    nextEl.appendChild(nextTitleEl);
    nextEl.appendChild(nextCountdownEl);

    face.appendChild(timeEl);
    face.appendChild(dateEl);
    face.appendChild(nextEl);
    container.appendChild(face);
```

(This replaces the existing three-line `face.appendChild(timeEl); ...
container.appendChild(face);` block - `nextEl` is new, `timeEl`/`dateEl`
already existed.)

At the top of `render()`, read the new setting the same way `style` is read:

```js
  style = (state && state.clockStyle) === "analogue" ? "analogue" : "digital";
  hideNextEvent = Boolean(state && state.hideNextEvent);
```

At the bottom of `render()`, where the existing clock source is replayed
(`const existing = state && state.sources && state.sources.clock; ...`), add
the same replay for calendar:

```js
  const existing = state && state.sources && state.sources.clock;
  if (existing && existing.data) {
    onState("clock", existing.data);
  }
  const calendar = state && state.sources && state.sources.calendar;
  if (calendar && calendar.data) {
    onState("calendar", calendar.data);
  }
```

**Step 3: Handle the calendar source in `onState`**

At the top of `onState`, before the existing `if (source !== "clock" ...)`
guard, add a calendar branch (ported from `overview.js`):

```js
export function onState(source, data) {
  if (source === "calendar") {
    calendarData = data && data.title ? data : null;
    calendarSyncedAt = Date.now();
    paintNext();
    return;
  }

  if (source !== "clock" || !data) {
    return;
  }
  // ... existing body unchanged ...
```

**Step 4: Add `paintNext`**

Add this function, ported from `overview.js` with one change: it now also
gates on `hideNextEvent`, and calls a new `paintDigitalNext`/
`paintAnalogueNext` split (analogue comes in Task 6 - for this task, only
wire the digital half; leave a `// TODO(Task 6): analogue` marker or just
call the digital painter unconditionally, since `analogueNextEl` doesn't
exist yet):

```js
function paintNext() {
  if (!nextEl) return;

  if (hideNextEvent || !calendarData) {
    nextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  nextEl.hidden = false;
  nextTitleEl.textContent = calendarData.title;
  nextCountdownEl.textContent = formatCountdown(minutesUntil);
  nextCountdownEl.className = `clock-next-countdown is-${urgency(minutesUntil)}`;
}
```

**Step 5: Clear state in `teardown`**

Add to `teardown()`:

```js
  nextEl = null;
  nextTitleEl = null;
  nextCountdownEl = null;
  calendarData = null;
```

**Step 6: Rename the CSS classes**

In `style.css`, rename `.overview-next` to `.clock-next`,
`.overview-next-title` to `.clock-next-title`,
`.overview-next-countdown` to `.clock-next-countdown` (the comment above the
block, and the `#panel[data-sleep="true"] .overview-next` sleep rule, need
the same rename):

```css
/* The clock face's next-up line, shown under the date when calendar
 * integration is on (hide_next_event false) and something is upcoming.
 * Hidden entirely (not just empty) when there is nothing scheduled, so an
 * idle calendar costs no vertical space under the date. */
.clock-next {
  display: flex;
  flex-direction: column;
  align-items: center;
  margin: 20px 0 0;
  font-size: 17px;
  max-width: 280px;
  margin-left: auto;
  margin-right: auto;
}

.clock-next-title {
  color: var(--text);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clock-next-countdown {
  margin-top: 4px;
  font-size: 15px;
  color: var(--live);
}

.clock-next-countdown.is-warn {
  color: var(--warn);
}

.clock-next-countdown.is-alert {
  color: var(--alert);
}
```

And in the sleep rule:

```css
#panel[data-sleep="true"] .clock-time,
#panel[data-sleep="true"] .clock-date,
#panel[data-sleep="true"] .clock-hands,
#panel[data-sleep="true"] .clock-next,
#panel[data-sleep="true"] .rim,
#panel[data-sleep="true"] #connection {
  display: none;
}
```

**Step 7: Manual verification (digital, calendar on)**

There's no unit test for this yet (`paintNext`'s DOM-coupling and
`Date.now()` use matches how `overview.js` was, deliberately left untested
at that layer - see Task 6 for the one new pure helper this plan does add
tests for). Verify by hand:

1. Serve `agent/web/` statically: `python -m http.server 8934` from that
   directory.
2. Open `http://localhost:8934/dev.html`, select the `clock` face.
3. In the browser console: `import("./faces/clock.js").then(m => { m.onState("calendar", { title: "Standup", minutes_until: 11, start_label: "10:04" }); })`
4. Expected: below the date, "Standup" and "in 11 min" appear, matching
   today's `overview` face exactly.

**Step 8: Commit**

```bash
git add agent/web/faces/clock.js agent/web/style.css
git commit -m "feat(clock): absorb the digital next-up line from overview"
```

---

## Task 6: Analogue + calendar layout

**Files:**
- Modify: `agent/web/faces/clock.js`
- Modify: `agent/web/style.css`
- Test: `agent/web/clock.test.js`

**Step 1: Write the failing test for the new pure helper**

The one piece of new logic worth a pure unit test is the analogue mode's
compact single-line label (`"title (dot) countdown"`). Add to
`clock.test.js`:

```js
import { handAngles, hourNumerals, tickMarks, compactNextEventLabel } from "./faces/clock.js";

test("compactNextEventLabel joins title and countdown with a middle dot", () => {
  assert.equal(
    compactNextEventLabel("Standup with the team", 11),
    "Standup with the team · in 11 min"
  );
});

test("compactNextEventLabel formats a countdown already at zero as now", () => {
  assert.equal(compactNextEventLabel("Standup", 0), "Standup · now");
});
```

**Step 2: Run to verify it fails**

Run: `node --test agent/web/clock.test.js`
Expected: FAIL - `compactNextEventLabel is not a function` (or `undefined`)

**Step 3: Implement the pure helper**

In `clock.js`, near the top with the other pure exports (`handAngles`,
`tickMarks`, `hourNumerals`):

```js
// compactNextEventLabel is the analogue face's one-line next-up text: no
// room for a stacked title-then-countdown block in the hub-to-numeral gap,
// so both live on one line, joined by a middle dot the way a watch's
// complications get abbreviated.
export function compactNextEventLabel(title, minutesUntil) {
  return `${title} · ${formatCountdown(minutesUntil)}`;
}
```

**Step 4: Run to verify it passes**

Run: `node --test agent/web/clock.test.js`
Expected: PASS

**Step 5: Build the analogue next-up DOM**

In `render()`'s `if (style === "analogue")` branch, the current code ends
with building `dateEl` directly into `container`. Replace that tail (from
the `// Not wrapped in .face...` comment onward) with a wrapper that holds
both the date and the new next-up line:

```js
    // Not wrapped in .face: .face is positioned and sized for the digital
    // layout's centred text block, but this info sits low in the circle
    // (or, once a next event is showing, in the open disc between the hub
    // and the numeral ring), clear of the hands, the same way .rim and
    // #connection are positioned straight off #panel rather than through
    // .face.
    const info = document.createElement("div");
    info.className = "clock-analogue-info";

    dateEl = document.createElement("p");
    dateEl.className = "clock-date-analogue";
    dateEl.textContent = "waiting for the agent";
    info.appendChild(dateEl);

    analogueNextEl = document.createElement("p");
    analogueNextEl.className = "clock-analogue-next";
    info.appendChild(analogueNextEl);

    container.appendChild(info);
    infoEl = info;
```

Add `analogueNextEl` and `infoEl` to the module-level `let` declarations
next to `nextEl`.

**Step 6: Split `paintNext` into digital/analogue painters**

Replace the `paintNext` function from Task 5 with:

```js
function paintNext() {
  if (style === "analogue") {
    paintAnalogueNext();
  } else {
    paintDigitalNext();
  }
}

function paintDigitalNext() {
  if (!nextEl) return;

  if (hideNextEvent || !calendarData) {
    nextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  nextEl.hidden = false;
  nextTitleEl.textContent = calendarData.title;
  nextCountdownEl.textContent = formatCountdown(minutesUntil);
  nextCountdownEl.className = `clock-next-countdown is-${urgency(minutesUntil)}`;
}

function paintAnalogueNext() {
  if (!analogueNextEl || !infoEl) return;

  if (hideNextEvent || !calendarData) {
    infoEl.classList.remove("has-next");
    analogueNextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  infoEl.classList.add("has-next");
  analogueNextEl.hidden = false;
  analogueNextEl.textContent = compactNextEventLabel(calendarData.title, minutesUntil);
  analogueNextEl.className = `clock-analogue-next is-${urgency(minutesUntil)}`;
}
```

**Step 7: Clear the new state in `teardown`**

Add to `teardown()`:

```js
  analogueNextEl = null;
  infoEl = null;
```

**Step 8: CSS for the analogue layout**

In `style.css`, near `.clock-date-analogue`, replace its positioning (which
moves to the new wrapper) and add the wrapper + next-line styles:

```css
/* The date is all .face holds in analogue mode (the hands take the centre),
 * so it sits low in the circle rather than dead centre, clear of the hands
 * at every hour - unless a next event is showing, in which case
 * .clock-analogue-info.has-next moves the whole block inward instead. */
.clock-analogue-info {
  position: absolute;
  left: 50%;
  bottom: 100px;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  margin: 0;
}

/* Moved into the open disc between the hub and the numeral ring, centred
 * in its lower half - furthest from the ticks and numerals, closest to
 * where a real watch's date-wheel complication sits. The hands (drawn
 * earlier in the DOM, so this renders on top of them) visibly sweep
 * behind this text as they pass, same as a watch complication. */
.clock-analogue-info.has-next {
  bottom: auto;
  top: 300px;
  transform: translate(-50%, -50%);
}

.clock-date-analogue {
  margin: 0;
  white-space: nowrap;
}

.clock-analogue-next {
  margin: 0;
  font-size: 13px;
  max-width: 220px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.clock-analogue-next.is-warn {
  color: var(--warn);
}

.clock-analogue-next.is-alert {
  color: var(--alert);
}
```

**Step 9: Update the sleep-hide rule**

`.clock-analogue-info` replaces the bare `.clock-date` selector's analogue
counterpart - check the existing sleep rule; if `.clock-date-analogue` was
never separately listed there (it wasn't - only `.clock-date` was, which is
the digital class), no change needed there. Do add `.clock-analogue-info` to
be safe, since it's now the positioned element:

```css
#panel[data-sleep="true"] .clock-time,
#panel[data-sleep="true"] .clock-date,
#panel[data-sleep="true"] .clock-hands,
#panel[data-sleep="true"] .clock-analogue-info,
#panel[data-sleep="true"] .clock-next,
#panel[data-sleep="true"] .rim,
#panel[data-sleep="true"] #connection {
  display: none;
}
```

**Step 10: Manual verification - all four combinations**

Using the same `dev.html` + browser console technique as Task 5's Step 7,
render `clock.js` with `state.clockStyle` set to each of
`"digital"`/`"analogue"` and `state.hideNextEvent` set to each of
`true`/`false`, feeding a mock calendar reading via `onState("calendar",
{...})`. Screenshot each of the four and check:

- digital, calendar on: identical to today's `overview` face.
- digital, calendar off: identical to today's plain `clock` digital mode.
- analogue, calendar off: identical to today's `clock` analogue mode (date
  pinned to the bottom edge, nothing else changed).
- analogue, calendar on: date + "Standup with the team (dot) in 11 min"
  stacked in the open disc between the hub and the 6 o'clock numeral, hands
  visibly crossing behind the text as the reading ticks forward, dot/tick
  clear of everything at the bottom edge (unchanged from the earlier
  session's clock-face work).

This is the one part of the whole plan with real visual risk - do not skip
it, and iterate on the `top: 300px` / font sizes in Step 8 if anything
collides with the numerals or looks cramped.

**Step 11: Commit**

```bash
git add agent/web/faces/clock.js agent/web/style.css agent/web/clock.test.js
git commit -m "feat(clock): show the next event on the analogue face too"
```

---

## Task 7: Delete `overview.js` and wire `app.js`

**Files:**
- Delete: `agent/web/faces/overview.js`
- Modify: `agent/web/app.js`

**Step 1: Delete the file**

```bash
git rm agent/web/faces/overview.js
```

**Step 2: Remove it from `ALL_FACES`**

In `app.js`, remove the import:

```js
import * as overviewFace from "./faces/overview.js";
```

Update the `ALL_FACES` line and its comment:

```js
// Every face that exists, in the order tapping cycles through when none are
// hidden. clock first because it is what the panel shows most of the time
// (and, with hide_next_event false, carries the next-up calendar line the
// same as the old dedicated overview face did); status last because it is
// the debug face. Titles here have to match config.KnownFaces on the agent
// side, which is what actually validates hidden_faces.
const ALL_FACES = [clockFace, calendarFace, spotifyFace, telemetryFace, statusFace];
```

**Step 3: Read `hide_next_event` from `/health`**

In `state`, add next to `clockStyle`:

```js
export const state = {
  connection: "connecting",
  uptimeSeconds: 0,
  version: "",
  accentColor: "",
  hiddenFaces: [],
  clockStyle: "digital",
  hideNextEvent: false,
  sources: {},
  lastError: "",
};
```

In `applyHealth`, next to the `state.clockStyle` line:

```js
  state.clockStyle = health.clock_style || "digital";
  state.hideNextEvent = Boolean(health.hide_next_event);
```

**Step 4: Re-render the clock face on a `hideNextEvent` change too**

Rename `syncClockStyle` to `syncClockSettings` (it now tracks two fields)
and extend it:

```js
let lastClockStyle = "digital";
let lastHideNextEvent = false;

// syncClockSettings re-renders the clock face when clock_style or
// hide_next_event changes under it, live. A plain onState update cannot do
// this: digital, analogue, and calendar-on/off build different DOM (text
// versus SVG hands, an extra next-up line or not), so a change needs the
// same teardown-then-render showFace already does, not just a new reading
// fed into whatever shape is already on screen.
function syncClockSettings() {
  if (state.clockStyle === lastClockStyle && state.hideNextEvent === lastHideNextEvent) {
    return;
  }
  lastClockStyle = state.clockStyle;
  lastHideNextEvent = state.hideNextEvent;
  if (currentFace && currentFace.title === "clock") {
    showFace(currentIndex);
  }
}
```

Update the one call site in `pollHealth`:

```js
    applyHealth(await response.json());
    applyAccentColor();
    syncFaces();
    syncClockSettings();
```

**Step 5: Run the JS test suite**

Run: `node --test agent/web/*.test.js`
Expected: PASS, all tests (check `app.test.js` doesn't reference
`overviewFace`/`ALL_FACES.length` with the old count of 6 - if it does,
update the expected length to 5)

**Step 6: Commit**

```bash
git add agent/web/app.js
git commit -m "feat(app): drop the overview face, wire hide_next_event"
```

---

## Task 8: Settings page

**Files:**
- Modify: `agent/web/settings.js`

**Step 1: Update the `FACES` table**

Remove the `overview` entry entirely (it's no longer a face - nothing to
toggle in the generic list, since `clock` already has no row there either,
per the earlier `fix/clock-face-not-hideable` work):

```js
const FACES = [
  { title: "calendar", label: "Calendar" },
  { title: "spotify", label: "Spotify" },
  { title: "telemetry", label: "Telemetry" },
  { title: "status", label: "Status" },
];
```

Update the file-level comment block above it (currently explains the
`overview` row's special placement) to describe the new
`hide_next_event` toggle instead:

```js
// FACES here has to name the same faces, by the same titles, as ALL_FACES
// in app.js and KnownFaces in the agent's config package - three places
// that have to agree, the same kind of one-line-per-thing table this
// codebase already accepts elsewhere (the source factory table, for one).
//
// clock has no row here (same as before) - there's nothing to toggle, it
// can't be hidden. Its two settings ("Analogue" and "Show next event")
// live under the Clock heading instead, built separately below.
```

**Step 2: Add the "Show next event" toggle**

Near where `analogueInput` is built in `start()`:

```js
  buildFaceToggles();
  analogueInput = buildToggle(clockEl, "clock-analogue", "Analogue");
  nextEventInput = buildToggle(clockEl, "clock-next-event", "Show next event");
  saveButton.addEventListener("click", save);
  loadCurrent();
```

Add `nextEventInput` to the module-level `let` declarations next to
`analogueInput`.

**Step 3: Load and save it, inverted**

In `loadCurrent`, next to the `analogueInput.checked` line:

```js
    analogueInput.checked = data.clock_style === "analogue";
    nextEventInput.checked = !data.hide_next_event;
```

In `save`, in the request body:

```js
      body: JSON.stringify({
        accent_color: accentInput.value,
        hidden_faces: hidden,
        clock_style: analogueInput.checked ? "analogue" : "digital",
        hide_next_event: !nextEventInput.checked,
      }),
```

**Step 4: Manual verification**

Serve `agent/web/` statically, open `settings.html`, confirm: no
"Combined clock/calendar face" or "Clock" row in the generic Faces list
(only Calendar/Spotify/Telemetry/Status), and a new "Show next event" toggle
sits under the Clock heading next to "Analogue".

**Step 5: Commit**

```bash
git add agent/web/settings.js
git commit -m "feat(settings): replace the overview toggle with show-next-event"
```

---

## Task 9: Docs

**Files:**
- Modify: `docs/architecture.md`

**Step 1: Update the face list and clock_style paragraph**

Replace the `Current faces, in tap order: ...` paragraph (lines ~392-404)
with:

```markdown
Current faces, in tap order: `clock`, `calendar`, `spotify`, `telemetry`,
`status`. `clock` comes first because it is what the panel shows most of
the time: time, date, and (with `hide_next_event` false) the next-up
calendar event, all together. `status` stays last because it is the debug
face: it lists every source with its status, last update, and last error,
along with agent uptime and WebSocket connection state, and is the fallback
whenever the WebSocket is down, so there is always something truthful on
screen.
```

Replace the `clock_style` paragraph (lines ~406-417) with:

```markdown
`clock_style` (`"digital"` or `"analogue"`, default `"digital"`) picks how
`clock` draws: digital text plus the rim's seconds arc, or hour/minute/
second hands. `hide_next_event` (bool, default `false`) picks whether the
next calendar event shows at all - independently of `clock_style`, so all
four combinations are supported. In digital mode the next-up line sits
below the date, in flow. In analogue mode there's no room in the bottom
edge's tick-and-connection-dot band for a second line, so a shown next
event moves the whole date-plus-next-up block inward, into the open disc
between the hub and the numeral ring, drawn on top of the hands (they
visibly sweep behind the text as they pass, the same as a watch's
date-wheel complication). The hand math (`handAngles` in `clock.js`) is
pure and unit tested separately from the DOM it drives, the same split
every other face's non-trivial logic gets. Drawn as a second full-panel SVG
alongside `.rim` rather than nested inside `.face`, for the same reason
`.calendar` and `.spotify` already override `.face`'s `transform`: anything
that has to draw across the whole circle needs to sit outside the stacking
context `.face`'s `translate(-50%, -50%)` creates, not inside it.
```

**Step 2: Update the Settings section**

Replace the `GET reports the currently configured ...` sentence (~466-472):

```markdown
`GET/POST /settings`, authenticated the same as everything else that is not
`/health` or an OAuth callback. GET reports the currently configured
`accent_color`, `hidden_faces`, `clock_style`, and `hide_next_event`; POST
validates all four together (a `"#rrggbb"` colour, face titles against
`config.KnownFaces`, not every face hidden at once, `clock_style` against
`config.ClockStyles`), writes them to `config.json` (`config.Save`, atomic,
mirrors the pattern spotify's `state_file` uses), and reloads. The shape is
generic enough that each new setting is an added field, not a restructure.
```

Replace the `The settings page ...` paragraph (~479-485):

```markdown
The settings page (`settings.html`/`settings.js`) groups `clock_style` and
`hide_next_event` under a "Clock" heading, labelled "Analogue" and "Show
next event", ahead of the generic "Faces" list for the rest. Neither is an
entry in `hidden_faces` - `clock` itself has no row in that list at all,
since it can't be hidden and there's nothing else for a toggle there to do.
```

Update `All three settings are carried on /health` (~495) to `All four
settings are carried on /health`, and check the sentence right after it
(`applyAccentColor` sets ..., `syncFaces` recomputes ...) for a third
function name (`syncClockStyle`) that needs to become `syncClockSettings` -
read the next ~5 lines after line 499 and update accordingly.

**Step 3: Check for other stale references**

Run: `grep -n "overview" docs/architecture.md` - anything left should only
be prose that still makes sense post-merge (there shouldn't be any; if
there is, fix it in place).

**Step 4: Commit**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): describe the merged clock/overview face"
```

---

## Task 10: Full verification pass

**Step 1: Full test suites**

Run from `agent/`: `go build ./... && go test ./...`
Expected: PASS, all packages

Run from `agent/web/`: `node --test *.test.js`
Expected: PASS, all tests

**Step 2: Re-check the four-combination screenshots from Task 6, Step 10**

If anything was left iterating, finish it now with the full app.js wiring
in place (Task 7) rather than the raw `onState` console poke from Task 6 -
use `dev.html`'s actual face-switching UI this time, with a temporary local
edit to its `scenarios` object adding a calendar reading (do not commit that
edit - it's dev.html scratch, revert it after screenshotting, or check
whether the harness is worth a permanent calendar scenario and ask before
committing one).

**Step 3: Grep for anything still referencing the retired face**

Run: `grep -rli overview agent shell docs README.md` (case-insensitive)
Expected: nothing left that describes the retired `overview` face as a
current, real thing (unrelated hits, if any, are fine - e.g. an unrelated
English use of the word).

**Step 4: Final commit if anything was left uncommitted**

```bash
git status
```

If clean, this plan is done.
