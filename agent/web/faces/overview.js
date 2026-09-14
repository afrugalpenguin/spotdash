// Overview face. The clock face's time and date, plus a compact next-up
// line underneath, so a glance at the time also shows what's coming without
// tapping through to the standalone calendar face. That face still exists
// separately for its own auto-switch/urgent behaviour; this one is just for
// having both at a glance together.
//
// The rim stays the clock's seconds tick, not event urgency: this is a
// clock face first, with calendar as the addition, so the rim keeps
// meaning what it means on the standalone clock face.

import { createRim, RIM_CIRCUMFERENCE, setArc } from "./rim.js";
import { urgency, formatCountdown } from "./calendar.js";

let timeEl = null;
let dateEl = null;
let nextEl = null;
let nextTitleEl = null;
let nextCountdownEl = null;
let arcEl = null;

// The calendar reading, kept only so paintNext can recompute the countdown
// on every clock tick (once a second) rather than running a second local
// ticker: the clock source already provides that heartbeat for free.
let calendarData = null;
let calendarSyncedAt = 0;

export function render(container, state) {
  container.innerHTML = "";

  const rim = createRim();
  arcEl = rim.arc;
  container.appendChild(rim.svg);

  const face = document.createElement("div");
  face.className = "face";

  timeEl = document.createElement("p");
  timeEl.className = "clock-time";
  timeEl.textContent = "--:--";

  dateEl = document.createElement("p");
  dateEl.className = "clock-date";
  dateEl.textContent = "waiting for the agent";

  nextEl = document.createElement("p");
  nextEl.className = "overview-next";
  nextTitleEl = document.createElement("span");
  nextTitleEl.className = "overview-next-title";
  nextCountdownEl = document.createElement("span");
  nextCountdownEl.className = "overview-next-countdown";
  nextEl.appendChild(nextTitleEl);
  nextEl.appendChild(nextCountdownEl);

  face.appendChild(timeEl);
  face.appendChild(dateEl);
  face.appendChild(nextEl);
  container.appendChild(face);

  const dot = document.createElement("div");
  dot.className = "sleep-dot";
  container.appendChild(dot);

  const clock = state && state.sources && state.sources.clock;
  if (clock && clock.data) {
    onState("clock", clock.data);
  }
  const calendar = state && state.sources && state.sources.calendar;
  if (calendar && calendar.data) {
    onState("calendar", calendar.data);
  }
}

export function onState(source, data) {
  if (source === "calendar") {
    calendarData = data && data.title ? data : null;
    calendarSyncedAt = Date.now();
    paintNext();
    return;
  }

  if (source !== "clock" || !data || !timeEl) {
    return;
  }

  timeEl.textContent = data.time || "--:--";
  dateEl.textContent = data.date || "";

  // Sleep is a panel-wide state, the same as the standalone clock face.
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = data.sleep ? "true" : "false";
  }

  const seconds = typeof data.seconds === "number" ? data.seconds : 0;
  setArc(arcEl, (seconds % 60) / 60);

  // Piggybacks on the clock's own once-a-second tick to keep the countdown
  // live, rather than running a second timer just for this.
  paintNext();
}

function paintNext() {
  if (!nextEl) return;

  if (!calendarData) {
    nextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  nextEl.hidden = false;
  nextTitleEl.textContent = calendarData.title;
  nextCountdownEl.textContent = formatCountdown(minutesUntil);
  nextCountdownEl.className = `overview-next-countdown is-${urgency(minutesUntil)}`;
}

export function teardown() {
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = "false";
  }
  timeEl = null;
  dateEl = null;
  nextEl = null;
  nextTitleEl = null;
  nextCountdownEl = null;
  arcEl = null;
  calendarData = null;
}

export const title = "overview";

export { RIM_CIRCUMFERENCE };
