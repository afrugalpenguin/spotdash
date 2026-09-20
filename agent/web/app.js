// Panel bootstrap: token handling, the live feed, and the face manager.
// Faces get the shared state object once in render and mutate from onState.

import * as clockFace from "./faces/clock.js";
import * as spotifyFace from "./faces/spotify.js";
import * as calendarFace from "./faces/calendar.js";
import * as telemetryFace from "./faces/telemetry.js";
import * as statusFace from "./faces/status.js";

// Tap order. Titles must match config.KnownFaces on the agent, which
// validates hidden_faces.
const ALL_FACES = [clockFace, calendarFace, spotifyFace, telemetryFace, statusFace];

// The visible subset. syncFaces reassigns it when settings change.
let FACES = ALL_FACES;
let FACE_NAMES = FACES.map((face) => face.title);

// visibleFaces drops the faces named in hiddenTitles. It returns allFaces if
// that would hide every face, in case a stale /health response disagrees
// with the config.
export function visibleFaces(allFaces, hiddenTitles) {
  const hidden = new Set(hiddenTitles || []);
  const visible = allFaces.filter((face) => !hidden.has(face.title));
  return visible.length > 0 ? visible : allFaces;
}

// Reconnection backoff starts fast for an agent restart and settles slowly
// for an agent gone for the evening.
const BACKOFF_MIN_MS = 500;
const BACKOFF_MAX_MS = 15000;
const HEALTH_INTERVAL_MS = 5000;

// A live socket can go silently stale. See docs/architecture.md,
// "Reconnection". The check interval must be well under the threshold.
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
  // Device clock minus agent clock, in ms. Zero until /health reports the
  // agent's time.
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
// Auto-switch state. urgentKey identifies the event holding the panel.
// The timer and return index are null when no switch is in flight.
let urgentKey = "";
let urgentHoldTimer = null;
let urgentReturnTo = null;

// readToken takes the token from the query string and removes it from the
// visible URL. It is never written to storage.
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

// jittered spreads reconnects so panels do not retry in lockstep.
export function jittered(delay) {
  return Math.round(delay * (0.5 + Math.random() * 0.5));
}

// isStale reports whether the socket has been quiet for over thresholdMs.
// A lastMessageAt of 0 means never connected, which is not stale.
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

// handleCalendarUrgency shows the calendar face for an urgent event, holds it
// for show_seconds, then returns to the previous face. It also wakes a
// sleeping panel. urgentKey stops the same event retriggering on every poll.
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
    // First switch of a hold only: a retrigger must not overwrite the
    // return index with "calendar".
    urgentReturnTo = currentIndex;
  }

  showFace(calendarIndex);
  if (panel) {
    panel.dataset.sleep = "false";
  }

  const holdMs = (data.show_seconds || 45) * 1000;
  urgentHoldTimer = window.setTimeout(() => {
    urgentHoldTimer = null;
    // Leave the panel alone if someone tapped away during the hold.
    if (currentIndex === calendarIndex && urgentReturnTo !== null) {
      showFace(urgentReturnTo);
    }
    urgentReturnTo = null;
  }, holdMs);
}

// reportFaceError contains a broken face. The message goes on screen, since
// nobody reads a console at the device, and to the console for logcat.
export function reportFaceError(err) {
  console.error(`face failed: ${err && err.message ? err.message : err}`);
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

// applyHealth folds a /health response into the state object. receivedAtMs
// is the device clock at arrival. See docs/architecture.md, "Where status
// comes from".
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

// applyAccentColor is the only place that touches document, which keeps
// applyHealth testable without a DOM.
function applyAccentColor() {
  if (!state.accentColor) {
    return;
  }
  document.documentElement.style.setProperty("--live", state.accentColor);
}

// syncFaces applies state.hiddenFaces to FACES. It does nothing when the set
// is unchanged, since a rebuild on every health poll would reset scroll
// position and the spotify timer.
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

  // Keep the current face if still visible, else go to the first.
  const stillVisible = FACES.findIndex((face) => face.title === currentTitle);
  showFace(stillVisible === -1 ? 0 : stillVisible);
}

let lastClockStyle = "digital";
let lastHideNextEvent = false;

// syncClockSettings re-renders the clock when its style or next-event line
// changes. Each variant builds different DOM, so onState cannot switch it.
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

  // A browser cannot set WebSocket headers, and a query parameter gets
  // logged, so the token rides as a subprotocol.
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
    // The clock face rewrites dataset.sleep every tick, so reassert the wake
    // for as long as the hold is active.
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

// checkStaleness closes a quiet socket so the close handler reconnects. It
// only acts while live, since lastMessageAt means nothing during a backoff.
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

  // Arrow keys are for desktop development. The device has no keyboard.
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
