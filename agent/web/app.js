// spotdash panel bootstrap: token handling, the live feed, and the face
// manager.
//
// The panel holds one state object and mutates it in place. Faces are handed
// that object once in render and are told when something changed, so no face
// has to re-read or copy it.

import * as clockFace from "./faces/clock.js";
import * as spotifyFace from "./faces/spotify.js";
import * as telemetryFace from "./faces/telemetry.js";
import * as statusFace from "./faces/status.js";

// Order is the order tapping cycles through. Clock first because it is what the
// panel shows most of the time, status last because it is the debug face.
const FACES = [clockFace, spotifyFace, telemetryFace, statusFace];
const FACE_NAMES = FACES.map((face) => face.title);

// Reconnection backoff. Starts fast because the common case is the agent
// restarting, and settles slowly because the other case is the agent being
// gone for the evening.
const BACKOFF_MIN_MS = 500;
const BACKOFF_MAX_MS = 15000;
const HEALTH_INTERVAL_MS = 5000;

export const state = {
  connection: "connecting",
  uptimeSeconds: 0,
  version: "",
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
export function applyHealth(health) {
  if (!health) {
    return;
  }
  state.uptimeSeconds = health.uptime_seconds || 0;
  state.version = health.version || "";

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

async function pollHealth() {
  try {
    const response = await fetch("/health", { cache: "no-store" });
    if (!response.ok) {
      throw new Error(`health returned ${response.status}`);
    }
    applyHealth(await response.json());
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
    setConnection("live");
  });

  socket.addEventListener("message", (event) => {
    let message;
    try {
      message = JSON.parse(event.data);
    } catch (err) {
      state.lastError = "received a message that was not JSON";
      return;
    }
    applyMessage(message);
    notifyFace(message.source, message.data);
  });

  socket.addEventListener("close", () => {
    setConnection("down");
    scheduleReconnect();
  });

  socket.addEventListener("error", () => {
    state.lastError = "socket error";
  });
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
  connect();
}

export { FACE_NAMES, FACES };
