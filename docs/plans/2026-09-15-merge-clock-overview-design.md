# Merge the overview and clock faces

## Problem

The panel has two time-telling faces today:

- `overview`: always digital, shows time + date + a next-up calendar line.
  Never respects `clock_style`.
- `clock`: respects `clock_style` (digital or analogue), never shows the
  calendar.

That's two faces competing for the same job, and no way to get an analogue
clock with the next event on it - the exact combination someone reaching for
an analogue style while also using the calendar integration would want.

## Design

Replace both with a single `clock` face, controlled by two independent
settings:

- `clock_style`: `"digital"` | `"analogue"` (unchanged)
- `hide_next_event` (new, bool, default `false` = shown): whether the next
  calendar event is hidden. Inverted rather than a `clock_calendar: bool`
  defaulting to `true`, because a plain Go `bool` can't distinguish "absent
  from config.json" from "explicitly false" on decode - the same reason
  `hidden_faces` is itself phrased as a negative, defaults-to-off list
  rather than a positive `visible_faces`. The settings page inverts it back
  for the checkbox, the same way it already inverts `hidden_faces` into
  each face's checked state.

All four combinations are supported. `overview` is retired entirely - not
hidden, not deprecated-but-present, gone from `KnownFaces`, `ALL_FACES`, and
the settings page's face list. The rotation drops from 6 faces to 5: `clock`,
`calendar`, `spotify`, `telemetry`, `status`.

### Digital mode

Digital + calendar-on is today's `overview` face, unchanged pixel-for-pixel:
time, date, next-up line, all stacked with room to spare. Digital +
calendar-off is today's plain `clock` digital mode. Both are a straight
relocation of existing, working code - zero layout risk.

### Analogue mode

Today the date sits pinned to the bottom edge, in a tight band between the
tick ring and the connection dot. Cramming a next-up line into that same
band would mean shrinking everything to fit.

Instead, when calendar integration is on, the date + next-up block moves
inward - into the open disc between the hub and the numeral ring, which is
otherwise empty space - and renders on top of the hands (same stacking the
date already uses today: it draws after the hands in the DOM, so hands pass
behind it). The block sits vertically centered in the lower half of that gap
(between the hub and the 6 o'clock numeral), furthest from the numerals and
ticks.

Hands will visibly sweep behind this text as they pass - the hour hand
especially, since it's short and spends a lot of its sweep in the inner
disc. That's intentional: it's how a real watch's date-wheel complication
behaves, and reads as deliberate rather than broken as long as the text
stays legible on top.

When calendar-off (or no event is upcoming), analogue mode is unchanged from
today: just the date, pinned to the bottom edge, in its current position.

## Config & wiring

- `config.go`: remove `"overview"` from `KnownFaces`. Add
  `HideNextEvent bool` (`json:"hide_next_event,omitempty"`), validated the
  same way every other field is (nothing to validate for a plain bool, but
  it goes through the same round-trip as `AccentColor`/`ClockStyle`).
- `app.go` / `server.go`: thread `HideNextEvent` through `Options`,
  `/health`, and the `/settings` GET/POST body the same way `ClockStyle`
  already is.
- `app.js`: drop `overviewFace` from `ALL_FACES`. Read
  `state.hideNextEvent` from `/health` (`health.hide_next_event || false`)
  the same way `state.clockStyle` is read, and extend the existing
  clock-style re-render trigger to also fire on a `hideNextEvent` change
  (both only matter to the `clock` face).
- `clock.js`: absorb `overview.js`'s `calendarData` /
  `calendarSyncedAt` / `paintNext` / countdown-urgency machinery. Render the
  next-up block in both digital and analogue branches, gated on
  `clockCalendar`.
- `overview.js` is deleted.
- Settings page: the "Combined clock/calendar face" row moves out of the
  hideable-faces list into the "Clock" section as a plain toggle next to
  "Analogue", relabelled "Show next event" (it no longer describes a whole
  face).

## Testing

- `clock.test.js`: extend with the ported next-up/countdown tests
  (currently implicitly covered via `app.test.js`'s references to the
  overview face's behaviour - check what actually exercises `paintNext` and
  port those cases, not just the DOM structure).
- `config_test.go`: `KnownFaces` no longer includes `"overview"`; add
  coverage for `clock_calendar` load/default/round-trip, mirroring the
  existing `clock_style` tests.
- `app_test.go` / `server_test.go`: update anything asserting on the old
  6-face `KnownFaces` list or exercising `hidden_faces` with `"overview"` in
  it.
- `static_test.go`: drop `/faces/overview.js` from the embedded-file
  existence list.
- Manual verification: render `clock.js` in `dev.html` for all four
  combinations (mock calendar event, both styles, calendar on/off) and
  screenshot each before calling it done - the analogue+calendar layout is
  the one piece of this with real visual risk.

## Docs

`docs/architecture.md` and `README.md` reference `overview` in a few places
(the "Settings" section, the face list) - update those to describe the
merged `clock` face and its two settings instead.
