// spotdash panel bootstrap: token handling, the live feed, and the face
// manager.
//
// The panel holds one state object and mutates it in place. Faces are handed
// that object once in render and are told when something changed, so no face
// has to re-read or copy it.

import * as clockFace from "./faces/clock.js";
import * as spotifyFace from "./faces/spotify.js";
import * as calendarFace from "./faces/calendar.js";
import * as telemetryFace from "./faces/telemetry.js";
import * as statusFace from "./faces/status.js";

// Every face that exists, in the order tapping cycles through when none are
// hidden. clock first because it is what the panel shows most of the time
// (and, with hide_next_event false, carries the next-up calendar line the
// same as the old dedicated overview face did); status last because it is
// the debug face. Titles here have to match config.KnownFaces on the agent
// side, which is what actually validates hidden_faces.
const ALL_FACES = [clockFace, calendarFace, spotifyFace, telemetryFace, statusFace];

// The active subset, filtered by state.hiddenFaces via syncFaces(). Plain
// `let` rather than const: which faces are active can change at runtime,
// from the settings page, without a page reload.
let FACES = ALL_FACES;
let FACE_NAMES = FACES.map((face) => face.title);

// visibleFaces filters allFaces down to whatever is not named in
// hiddenTitles. Falls back to allFaces if that would hide every face:
// config.Validate already refuses to save a hidden_faces that hides
// everything, but this stays defensive against, say, an old cached /health
// response briefly disagreeing with a config that has since changed.
export function visibleFaces(allFaces, hiddenTitles) {
  const hidden = new Set(hiddenTitles || []);
  const visible = allFaces.filter((face) => !hidden.has(face.title));
  return visible.length > 0 ? visible : allFaces;
}

// Reconnection backoff. Starts fast because the common case is the agent
// restarting, and settles slowly because the other case is the agent being
// gone for the evening.
const BACKOFF_MIN_MS = 500;
const BACKOFF_MAX_MS = 15000;
const HEALTH_INTERVAL_MS = 5000;

// A live connection has been observed, on this device, to go silently stale:
// the socket never fires close or error, the status face keeps reporting
// "live", and some sources keep updating while at least one stops, with no
// visible sign anything is wrong. Never fully explained (WebView
// backgrounding is the leading suspect), so this does not try to prevent it
// - it detects and recovers instead. STALE_CHECK_MS has to be well under
// STALE_THRESHOLD_MS so the check actually gets a few chances to run before
// the threshold is reached.
const STALE_THRESHOLD_MS = 60000;
const STALE_CHECK_MS = 10000;

export const state = {
  connection: "connecting",
  uptimeSeconds: 0,
  version: "",
  accentColor: "",
  hiddenFaces: [],
  clockStyle: "digital",
  hideNextEvent: false,
  // How far the device clock is ahead of the agent's, in ms (negative when it
  // is behind). Worked out from /health; subtracted from Date.now() before an
  // age is shown, so a skewed device clock does not make healthy sources look
  // stale. Zero until the agent has reported its time.
  clockOffsetMs: 0,
  sources: {},
  lastError: "",
};

let token = "";
let currentFace = null;
let currentIndex = 0;
let panel = null;
let faceHost = null;
let connectionEl = null;
let socket = null;
let backoffMs = BACKOFF_MIN_MS;
let reconnectTimer = null;
let lastMessageAt = 0;
// Auto-switch on an imminent calendar event. urgentKey identifies the event
// currently holding the panel, so a reading that is still the same urgent
// event (just a lower countdown) does not retrigger the switch on every
// poll; a different key (a new event went urgent, or the same title moved to
// a different start time) does. urgentHoldTimer and urgentReturnTo track the
// pending revert; both are null when no auto-switch is in flight.
let urgentKey = "";
let urgentHoldTimer = null;
let urgentReturnTo = null;

// readToken takes the token from the query string, holds it in memory, and
// removes it from the visible URL. It is never written to storage: a kiosk
// panel that persists a bearer token is a panel that leaks it to anyone who
// opens the browser later.
export function readToken(location, history) {
  const url = new URL(location.href);
  const value = url.searchParams.get("token") || "";
  if (!value) {
    return "";
  }
  url.searchParams.delete("token");
  if (history && typeof history.replaceState === "function") {
    history.replaceState(null, "", url.pathname + url.search + url.hash);
  }
  return value;
}

export function faceIndexFromQuery(search, names) {
  const requested = new URLSearchParams(search).get("face");
  if (!requested) {
    return 0;
  }
  const index = names.indexOf(requested);
  return index === -1 ? 0 : index;
}

export function nextBackoff(current) {
  return Math.min(BACKOFF_MAX_MS, Math.max(BACKOFF_MIN_MS, current * 2));
}

// jitter spreads reconnection attempts so several panels do not retry in
// lockstep against an agent that has just come back.
export function jittered(delay) {
  return Math.round(delay * (0.5 + Math.random() * 0.5));
}

// isStale reports whether too long has passed since anything was last heard
// on the socket, given how the connect open handler and every message both
// count as "heard from it". lastMessageAt of 0 means never connected, which
// is not staleness, just not there yet - a real connection attempt is
// already in flight or about to be, and this is not its job to chase.
export function isStale(lastMessageAt, now, thresholdMs) {
  if (lastMessageAt === 0) {
    return false;
  }
  return now - lastMessageAt > thresholdMs;
}

export function showFace(index) {
  if (!faceHost) {
    return;
  }
  const count = FACES.length;
  currentIndex = ((index % count) + count) % count;
  const face = FACES[currentIndex];

  if (currentFace && typeof currentFace.teardown === "function") {
    try {
      currentFace.teardown();
    } catch (err) {
      // A broken teardown must not block the switch.
      reportFaceError(err);
    }
  }

  currentFace = face;
  try {
    face.render(faceHost, state);
  } catch (err) {
    currentFace = null;
    reportFaceError(err);
  }
}

// handleCalendarUrgency switches to the calendar face when a reading says an
// event is urgent, holds it for the reading's own show_seconds, then returns
// to whatever face was showing before. Overrides sleep for the duration: an
// imminent event is worth waking the panel for, the same as it is worth
// interrupting whatever face was up.
//
// urgentKey guards against retriggering on every poll while the same event
// stays urgent; a genuinely new urgent event (different title or start time)
// gets its own switch.
function handleCalendarUrgency(data) {
  if (!data || !data.urgent) {
    return;
  }
  const key = `${data.title}|${data.start_label}`;
  if (key === urgentKey) {
    return;
  }
  urgentKey = key;

  const calendarIndex = FACES.findIndex((face) => face.title === "calendar");
  if (calendarIndex === -1) {
    return;
  }

  if (urgentHoldTimer !== null) {
    window.clearTimeout(urgentHoldTimer);
  } else {
    // Only remember where to return to on the first switch of a hold; a
    // retrigger mid-hold (a different event going urgent while the panel is
    // already showing this one) must not overwrite it with "calendar".
    urgentReturnTo = currentIndex;
  }

  showFace(calendarIndex);
  if (panel) {
    panel.dataset.sleep = "false";
  }

  const holdMs = (data.show_seconds || 45) * 1000;
  urgentHoldTimer = window.setTimeout(() => {
    urgentHoldTimer = null;
    // Only revert if the panel is still showing the face this hold put it
    // on. If someone tapped away during the hold, leave them where they are
    // rather than yanking them back.
    if (currentIndex === calendarIndex && urgentReturnTo !== null) {
      showFace(urgentReturnTo);
    }
    urgentReturnTo = null;
  }, holdMs);
}

// reportFaceError contains a broken face rather than letting it take the panel
// down. The message goes on screen because there is no console on the device.
function reportFaceError(err) {
  if (!faceHost) {
    return;
  }
  faceHost.innerHTML = "";
  const box = document.createElement("div");
  box.className = "face face-error";
  box.textContent = `face failed: ${err && err.message ? err.message : err}`;
  faceHost.appendChild(box);
}

function notifyFace(source, data) {
  if (!currentFace || typeof currentFace.onState !== "function") {
    return;
  }
  try {
    currentFace.onState(source, data);
  } catch (err) {
    reportFaceError(err);
  }
}

function setConnection(next) {
  state.connection = next;
  if (connectionEl) {
    connectionEl.dataset.state = next;
  }
  notifyFace("connection", { state: next });
}

// applyMessage folds one live message into the state object.
export function applyMessage(message) {
  if (!message || !message.source) {
    return;
  }
  const existing = state.sources[message.source] || {};
  state.sources[message.source] = {
    ...existing,
    status: existing.status || "ok",
    lastUpdate: message.ts || existing.lastUpdate || "",
    data: message.data,
  };
}

// applyHealth folds a /health response into the state object.
//
// Status, uptime, and last error come from /health rather than the socket,
// because /health needs no auth and keeps answering when the socket is down.
// That is exactly when the status face has to be truthful.
//
// receivedAtMs is the device clock when the response arrived. The offset it
// gives includes the response's travel time, which is milliseconds on a LAN
// and well under the whole seconds ages are shown in.
export function applyHealth(health, receivedAtMs = Date.now()) {
  if (!health) {
    return;
  }
  const agentNow = Date.parse(health.now || "");
  if (!Number.isNaN(agentNow)) {
    state.clockOffsetMs = receivedAtMs - agentNow;
  }
  state.uptimeSeconds = health.uptime_seconds || 0;
  state.version = health.version || "";
  state.accentColor = health.accent_color || "";
  state.hiddenFaces = health.hidden_faces || [];
  state.clockStyle = health.clock_style || "digital";
  state.hideNextEvent = Boolean(health.hide_next_event);

  const reported = health.sources || {};
  for (const name of Object.keys(reported)) {
    const existing = state.sources[name] || {};
    state.sources[name] = {
      ...existing,
      status: reported[name].status || "unknown",
      lastUpdate: reported[name].last_update || existing.lastUpdate || "",
      lastError: reported[name].last_error || "",
    };
  }
}

// applyAccentColor pushes the configured accent onto the document, live,
// without a page reload. Kept separate from applyHealth, which is plain
// state and unit tested without a DOM: this is the one place that touches
// document, and only ever called from the real browser bootstrap below.
function applyAccentColor() {
  if (!state.accentColor) {
    return;
  }
  document.documentElement.style.setProperty("--live", state.accentColor);
}

// syncFaces applies state.hiddenFaces to the active FACES list, live,
// without a page reload - the same "settings page saves, panel picks it up
// on its next /health poll" pattern the accent colour already uses. A
// no-op, deliberately, whenever the visible set has not actually changed:
// this runs on every health poll (every few seconds), and rebuilding the
// current face's DOM that often for no reason would reset things like the
// calendar agenda's scroll position and interrupt spotify's ticking timer.
function syncFaces() {
  const next = visibleFaces(ALL_FACES, state.hiddenFaces);
  const sameSet =
    next.length === FACES.length && next.every((face, i) => face === FACES[i]);
  if (sameSet) {
    return;
  }

  const currentTitle = currentFace ? currentFace.title : null;
  FACES = next;
  FACE_NAMES = FACES.map((face) => face.title);

  // Stay on the same face if it is still visible, otherwise land on
  // whatever is now first rather than an index that may no longer mean the
  // same face, or may not exist at all in a shorter list.
  const stillVisible = FACES.findIndex((face) => face.title === currentTitle);
  showFace(stillVisible === -1 ? 0 : stillVisible);
}

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

async function pollHealth() {
  try {
    const response = await fetch("/health", { cache: "no-store" });
    if (!response.ok) {
      throw new Error(`health returned ${response.status}`);
    }
    applyHealth(await response.json());
    applyAccentColor();
    syncFaces();
    syncClockSettings();
    state.lastError = "";
  } catch (err) {
    state.lastError = err && err.message ? err.message : String(err);
  }
  notifyFace("health", null);
}

function connect() {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${scheme}//${window.location.host}/ws`;

  setConnection("connecting");

  // The token travels as a subprotocol rather than a query parameter. A
  // browser cannot set headers on a WebSocket, and a query parameter would put
  // the secret somewhere it can be logged.
  try {
    socket = new WebSocket(url, ["spotdash.v1", `bearer.${token}`]);
  } catch (err) {
    state.lastError = err && err.message ? err.message : String(err);
    scheduleReconnect();
    return;
  }

  socket.addEventListener("open", () => {
    backoffMs = BACKOFF_MIN_MS;
    lastMessageAt = Date.now();
    setConnection("live");
  });

  socket.addEventListener("message", (event) => {
    lastMessageAt = Date.now();
    let message;
    try {
      message = JSON.parse(event.data);
    } catch (err) {
      state.lastError = "received a message that was not JSON";
      return;
    }
    applyMessage(message);
    notifyFace(message.source, message.data);
    if (message.source === "calendar") {
      handleCalendarUrgency(message.data);
    }
    // The clock face writes panel.dataset.sleep on every one of its own
    // ticks, which would otherwise put a sleeping panel back to sleep a
    // second after an urgent switch woke it. Reassert the override for as
    // long as the hold is active, regardless of which source just updated.
    if (urgentHoldTimer !== null && panel) {
      panel.dataset.sleep = "false";
    }
  });

  socket.addEventListener("close", () => {
    setConnection("down");
    scheduleReconnect();
  });

  socket.addEventListener("error", () => {
    state.lastError = "socket error";
  });
}

// checkStaleness closes a connection that has gone quiet for too long, so
// the existing close handler's setConnection("down") and scheduleReconnect
// take over from there - one recovery path rather than two. Only acts while
// the panel believes it is live: a connection already reconnecting through
// the normal backoff path needs no help here, and lastMessageAt is not
// meaningful yet during that window anyway.
function checkStaleness() {
  if (state.connection !== "live") {
    return;
  }
  if (isStale(lastMessageAt, Date.now(), STALE_THRESHOLD_MS)) {
    state.lastError = "connection went quiet, reconnecting";
    if (socket) {
      socket.close();
    }
  }
}

function scheduleReconnect() {
  if (reconnectTimer) {
    return;
  }
  const delay = jittered(backoffMs);
  backoffMs = nextBackoff(backoffMs);
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, delay);
}

function wireInput() {
  const prev = document.createElement("button");
  prev.className = "zone zone-prev";
  prev.type = "button";
  prev.setAttribute("aria-label", "previous face");
  prev.addEventListener("click", () => showFace(currentIndex - 1));

  const next = document.createElement("button");
  next.className = "zone zone-next";
  next.type = "button";
  next.setAttribute("aria-label", "next face");
  next.addEventListener("click", () => showFace(currentIndex + 1));

  panel.appendChild(prev);
  panel.appendChild(next);

  // Arrow keys are for developing on a desktop. The device has no keyboard.
  window.addEventListener("keydown", (event) => {
    if (event.key === "ArrowLeft") {
      showFace(currentIndex - 1);
    } else if (event.key === "ArrowRight") {
      showFace(currentIndex + 1);
    }
  });
}

export function start() {
  panel = document.getElementById("panel");
  faceHost = document.getElementById("faces");
  connectionEl = document.getElementById("connection");

  token = readToken(window.location, window.history);
  wireInput();
  showFace(faceIndexFromQuery(window.location.search, FACE_NAMES));

  pollHealth();
  window.setInterval(pollHealth, HEALTH_INTERVAL_MS);
  window.setInterval(checkStaleness, STALE_CHECK_MS);
  connect();
}

export { FACE_NAMES, FACES };
