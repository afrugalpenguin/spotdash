// Calendar face. The next-up event is the whole point, front and centre in
// its own card; a short agenda of what follows sits below it, quieter, for
// "what else is coming" without turning this into a calendar app on a 480px
// circle.
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
let pillEl = null;
let titleEl = null;
let locationEl = null;
let timeEl = null;
let countdownEl = null;
let agendaEl = null;
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

  // Title, time and countdown sit together in one card, the way a single
  // event reads in a calendar app's day view, rather than as bare text
  // floating in the circle.
  pillEl = document.createElement("div");
  pillEl.className = "calendar-pill";

  titleEl = document.createElement("p");
  titleEl.className = "calendar-title calendar-idle";
  titleEl.textContent = "Nothing scheduled";

  // Whatever the calendar itself put in the event's location, shown exactly
  // as the source gave it rather than reformatted, the same way spotify's
  // artist and album lines pass their fields through untouched.
  locationEl = document.createElement("p");
  locationEl.className = "calendar-location";

  timeEl = document.createElement("p");
  timeEl.className = "calendar-time";

  countdownEl = document.createElement("p");
  countdownEl.className = "calendar-countdown";

  pillEl.appendChild(titleEl);
  pillEl.appendChild(locationEl);
  pillEl.appendChild(timeEl);
  pillEl.appendChild(countdownEl);
  face.appendChild(pillEl);

  // The rest of the agenda: quieter than the pill, below it, empty (and
  // taking no space) whenever there is nothing after the primary event.
  agendaEl = document.createElement("ul");
  agendaEl.className = "calendar-agenda";
  face.appendChild(agendaEl);

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
  pillEl.className = "calendar-pill is-idle";
  titleEl.className = "calendar-title calendar-idle";
  titleEl.textContent = "Nothing scheduled";
  locationEl.textContent = "";
  timeEl.textContent = "";
  countdownEl.textContent = "";
  countdownEl.className = "calendar-countdown";
  agendaEl.innerHTML = "";
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
  pillEl.className = `calendar-pill is-${level}`;

  const fraction = 1 - Math.min(Math.max(minutesUntil, 0), LOOKAHEAD_MINUTES) / LOOKAHEAD_MINUTES;
  setArc(arcEl, fraction);
  arcEl.classList.remove("is-warn", "is-alert", "is-off");
  if (level === "warn") arcEl.classList.add("is-warn");
  if (level === "alert") arcEl.classList.add("is-alert");
}

// renderAgenda fills in what follows the primary event: time and title only,
// no location or countdown, since those stay specific to the one event the
// rim and auto-switch actually key off.
function renderAgenda(items) {
  agendaEl.innerHTML = "";
  for (const item of items || []) {
    const row = document.createElement("li");
    row.className = "calendar-agenda-row";

    const time = document.createElement("span");
    time.className = "calendar-agenda-time";
    time.textContent = item.start_label || "";

    const title = document.createElement("span");
    title.className = "calendar-agenda-title";
    title.textContent = item.title || "";

    row.appendChild(time);
    row.appendChild(title);
    agendaEl.appendChild(row);
  }
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
  locationEl.textContent = data.location || "";
  timeEl.textContent = data.start_label || "";
  renderAgenda(data.upcoming);

  paint(Math.round(data.minutes_until));
  startTicking();
}

export function teardown() {
  stopTicking();
  current = null;
  arcEl = null;
  pillEl = null;
  titleEl = null;
  locationEl = null;
  timeEl = null;
  countdownEl = null;
  agendaEl = null;
}

export const title = "calendar";
