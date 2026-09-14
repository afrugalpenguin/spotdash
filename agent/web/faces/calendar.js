// Calendar face. Shows exactly one thing: the next-up event. No agenda, no
// list, because the whole point of this face is answering "what's next"
// at a glance, not being a calendar app on a 480px circle.
//
// The rim carries urgency rather than a fixed quantity: it fills as the event
// approaches within a lookahead window, and its colour steps from live to
// warn to alert the same way the telemetry gauges do, so "getting close"
// reads the same language as everywhere else on the panel.

import { createRim, setArc } from "./rim.js";

// The rim's lookahead window. An event further out than this shows an empty
// rim rather than a barely-moving one; a ring that has visibly not started
// filling yet is more honest than one implying "soon" for something hours
// away.
const LOOKAHEAD_MINUTES = 60;
// Below this many minutes the countdown and rim read as urgent (alert).
const ALERT_MINUTES = 5;
// Below this many minutes they read as approaching (warn). Independent of,
// though normally equal to or above, the panel-wide auto-switch threshold
// configured on the source: this is purely a visual cue on the face itself.
const WARN_MINUTES = 15;

let arcEl = null;
let titleEl = null;
let timeEl = null;
let countdownEl = null;
let current = null; // the last reading, for the ticker to interpolate from
let syncedAt = 0; // Date.now() when `current` arrived
let ticker = null;

const TICK_MS = 1000;

export function render(container, state) {
  container.innerHTML = "";

  const rim = createRim();
  arcEl = rim.arc;
  container.appendChild(rim.svg);

  const face = document.createElement("div");
  face.className = "face calendar";

  titleEl = document.createElement("p");
  titleEl.className = "calendar-title calendar-idle";
  titleEl.textContent = "Nothing scheduled";

  timeEl = document.createElement("p");
  timeEl.className = "calendar-time";

  countdownEl = document.createElement("p");
  countdownEl.className = "calendar-countdown";

  face.appendChild(titleEl);
  face.appendChild(timeEl);
  face.appendChild(countdownEl);
  container.appendChild(face);

  const existing = state && state.sources && state.sources.calendar;
  if (existing && existing.data) {
    onState("calendar", existing.data);
  } else {
    showIdle();
  }
}

function showIdle() {
  stopTicking();
  current = null;
  if (!titleEl) return;
  titleEl.className = "calendar-title calendar-idle";
  titleEl.textContent = "Nothing scheduled";
  timeEl.textContent = "";
  countdownEl.textContent = "";
  countdownEl.className = "calendar-countdown";
  setArc(arcEl, 0);
  arcEl.classList.remove("is-warn", "is-alert");
  arcEl.classList.add("is-off");
}

// urgency classifies minutesUntil into the same three-step vocabulary the
// telemetry gauges use, so a viewer does not have to learn a second meaning
// for orange and red on this panel.
export function urgency(minutesUntil) {
  if (minutesUntil <= ALERT_MINUTES) return "alert";
  if (minutesUntil <= WARN_MINUTES) return "warn";
  return "live";
}

export function formatCountdown(minutesUntil) {
  if (minutesUntil <= 0) return "now";
  if (minutesUntil < 60) return `in ${minutesUntil} min`;
  const hours = Math.floor(minutesUntil / 60);
  const mins = minutesUntil % 60;
  return mins === 0 ? `in ${hours}h` : `in ${hours}h ${mins}m`;
}

function paint(minutesUntil) {
  const level = urgency(minutesUntil);
  countdownEl.textContent = formatCountdown(minutesUntil);
  countdownEl.className = `calendar-countdown is-${level}`;

  const fraction = 1 - Math.min(Math.max(minutesUntil, 0), LOOKAHEAD_MINUTES) / LOOKAHEAD_MINUTES;
  setArc(arcEl, fraction);
  arcEl.classList.remove("is-warn", "is-alert", "is-off");
  if (level === "warn") arcEl.classList.add("is-warn");
  if (level === "alert") arcEl.classList.add("is-alert");
}

function stopTicking() {
  if (ticker !== null) {
    clearInterval(ticker);
    ticker = null;
  }
}

function startTicking() {
  stopTicking();
  ticker = setInterval(() => {
    if (!current) return;
    const elapsedMinutes = (Date.now() - syncedAt) / 60000;
    const minutesUntil = Math.round(current.minutes_until - elapsedMinutes);
    paint(minutesUntil);
  }, TICK_MS);
}

export function onState(source, data) {
  if (source === "connection") {
    if (data && data.state === "live" && current) {
      startTicking();
    } else {
      stopTicking();
    }
    return;
  }

  if (source !== "calendar" || !titleEl) {
    return;
  }

  if (!data || !data.title) {
    showIdle();
    return;
  }

  current = data;
  syncedAt = Date.now();

  titleEl.className = "calendar-title";
  titleEl.textContent = data.title;
  timeEl.textContent = data.start_label || "";

  paint(Math.round(data.minutes_until));
  startTicking();
}

export function teardown() {
  stopTicking();
  current = null;
  arcEl = null;
  titleEl = null;
  timeEl = null;
  countdownEl = null;
}

export const title = "calendar";
