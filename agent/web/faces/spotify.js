// Spotify face: what is playing, with the cover behind it and the track
// position on the rim. Two layouts, fill and disc. See layoutFromQuery.

import { createRim, setArc } from "./rim.js";

let root = null;
let artHost = null;
let titleEl = null;
let artistEl = null;
let albumEl = null;
let elapsedEl = null;
let totalEl = null;
let stateEl = null;
let arcEl = null;
let shownArt = "";
let controlsEl = null;
let playPauseIconEl = null;
let currentlyPlaying = false;

const SVG_NS = "http://www.w3.org/2000/svg";

// Readings arrive every few seconds because the agent polls Spotify. The face
// carries the counter in between and resyncs on each reading.
const TICK_MS = 1000;
let ticker = null;
let current = null;
let syncedAt = 0;

// formatTime renders a track position as m:ss.
export function formatTime(ms) {
  if (typeof ms !== "number" || Number.isNaN(ms) || ms < 0) {
    return "0:00";
  }
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

// progressFraction is how far through the track we are, 0 to 1.
export function progressFraction(positionMs, durationMs) {
  if (
    typeof positionMs !== "number" ||
    typeof durationMs !== "number" ||
    !Number.isFinite(positionMs) ||
    !Number.isFinite(durationMs) ||
    durationMs <= 0
  ) {
    return 0;
  }
  return Math.max(0, Math.min(1, positionMs / durationMs));
}

// isPlayable reports whether there is a track worth rendering.
export function isPlayable(data) {
  return Boolean(data && typeof data.title === "string" && data.title !== "");
}

// playPauseAction is which control action the toggle button sends next, given
// whether playback is currently reported as playing.
export function playPauseAction(playing) {
  return playing ? "pause" : "resume";
}

// advancePosition moves the counter on by the time elapsed, looping back to the
// start at the end of the track.
export function advancePosition(positionMs, elapsedMs, durationMs) {
  if (typeof durationMs !== "number" || !Number.isFinite(durationMs) || durationMs <= 0) {
    return 0;
  }
  const from = typeof positionMs === "number" && Number.isFinite(positionMs) ? positionMs : 0;
  const by = typeof elapsedMs === "number" && Number.isFinite(elapsedMs) ? elapsedMs : 0;
  const next = (from + by) % durationMs;
  return next < 0 ? 0 : next;
}

// svgIcon builds one small flat icon from a list of path "d" attributes.
function svgIcon(paths) {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "22");
  svg.setAttribute("height", "22");
  for (const d of paths) {
    const path = document.createElementNS(SVG_NS, "path");
    path.setAttribute("d", d);
    svg.appendChild(path);
  }
  return svg;
}

// Transport glyphs as SVG paths, so they scale and take colour from CSS.
function previousIcon() {
  return svgIcon(["M6 5h2v14H6z", "M20 5L9 12l11 7z"]);
}
function nextIcon() {
  return svgIcon(["M16 5h2v14h-2z", "M4 5l11 7-11 7z"]);
}
function playIcon() {
  return svgIcon(["M6 4l14 8-14 8z"]);
}
function pauseIcon() {
  return svgIcon(["M5 4h5v16H5z", "M14 4h5v16h-5z"]);
}

// buildControlButton makes one round tap target holding an icon. It is sized
// well past 44px because the real digitiser is less precise than the
// emulator's.
function buildControlButton(label, icon) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "spotify-control";
  button.setAttribute("aria-label", label);
  button.appendChild(icon);
  return button;
}

// sendControl posts one transport command and reports whether it was
// accepted. Callers wait for the response, which avoids a revert-on-failure
// state machine.
async function sendControl(action) {
  try {
    const response = await fetch("/spotify/control", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    });
    return response.ok;
  } catch {
    return false;
  }
}

// flashDenied shows that a tap registered but the action failed, most often
// for want of an active device.
function flashDenied(button) {
  button.classList.add("is-denied");
  setTimeout(() => button.classList.remove("is-denied"), 600);
}

// The cover filling the panel is the default. ?layout=disc keeps the type on
// flat black, for a bright or busy sleeve where the scrim struggles.
function layoutFromQuery(search) {
  return new URLSearchParams(search || "").get("layout") === "disc"
    ? "disc"
    : "fill";
}

export function render(container, state) {
  container.innerHTML = "";
  shownArt = "";

  root = document.createElement("div");
  root.className = "face spotify";
  root.dataset.layout = layoutFromQuery(
    typeof window === "undefined" ? "" : window.location.search
  );

  artHost = document.createElement("div");
  artHost.className = "spotify-art";

  const scrim = document.createElement("div");
  scrim.className = "spotify-scrim";

  const content = document.createElement("div");
  content.className = "spotify-content";

  titleEl = document.createElement("p");
  titleEl.className = "spotify-title";

  // The transport row needs a stacking order above the .zone tap zones
  // (z-index 3), or a tap would switch faces. z-index needs position:
  // relative on a flex child.
  controlsEl = document.createElement("div");
  controlsEl.className = "spotify-controls";

  const prevBtn = buildControlButton("previous track", previousIcon());
  playPauseIconEl = playIcon();
  const playPauseBtn = buildControlButton("play or pause", playPauseIconEl);
  const nextBtn = buildControlButton("next track", nextIcon());

  prevBtn.addEventListener("click", async () => {
    if (!(await sendControl("previous"))) flashDenied(prevBtn);
  });
  nextBtn.addEventListener("click", async () => {
    if (!(await sendControl("next"))) flashDenied(nextBtn);
  });
  playPauseBtn.addEventListener("click", async () => {
    const action = playPauseAction(currentlyPlaying);
    if (await sendControl(action)) {
      // Apply the known outcome now. A button that lags its tap reads as broken.
      setPlayingIcon(!currentlyPlaying);
    } else {
      flashDenied(playPauseBtn);
    }
  });

  controlsEl.appendChild(prevBtn);
  controlsEl.appendChild(playPauseBtn);
  controlsEl.appendChild(nextBtn);

  artistEl = document.createElement("p");
  artistEl.className = "spotify-artist";

  albumEl = document.createElement("p");
  albumEl.className = "spotify-album";

  const times = document.createElement("div");
  times.className = "spotify-times";
  elapsedEl = document.createElement("span");
  totalEl = document.createElement("span");
  times.appendChild(elapsedEl);
  times.appendChild(totalEl);

  stateEl = document.createElement("p");
  stateEl.className = "spotify-state";

  content.appendChild(titleEl);
  content.appendChild(controlsEl);
  content.appendChild(artistEl);
  content.appendChild(albumEl);
  content.appendChild(times);
  content.appendChild(stateEl);

  root.appendChild(artHost);
  root.appendChild(scrim);
  root.appendChild(content);
  container.appendChild(root);

  // The rim goes on last so progress reads over the artwork.
  const rim = createRim();
  arcEl = rim.arc;
  root.appendChild(rim.svg);

  const existing = state && state.sources && state.sources.spotify;
  if (existing && existing.data) {
    onState("spotify", existing.data);
  } else {
    showIdle();
  }
}

function showIdle() {
  if (!root) return;
  stopTicking();
  current = null;
  root.classList.remove("is-paused");
  root.dataset.art = "none";
  artHost.innerHTML = "";
  shownArt = "";
  // Nothing to target: no active playback for a control action to reach.
  controlsEl.hidden = true;
  titleEl.className = "spotify-idle";
  titleEl.textContent = "Nothing playing";
  artistEl.textContent = "";
  albumEl.textContent = "";
  elapsedEl.textContent = "";
  totalEl.textContent = "";
  stateEl.textContent = "";
  setArc(arcEl, 0);
  arcEl.classList.add("is-off");
}

// setArt swaps the cover only on change. Decoding a 480px image every second
// is real work on a MediaTek SoC.
function setArt(url) {
  if (url === shownArt) return;
  shownArt = url;
  artHost.innerHTML = "";
  if (!url) {
    root.dataset.art = "none";
    return;
  }
  const img = document.createElement("img");
  img.alt = "";
  // A cover that fails to load leaves the type on black.
  img.addEventListener("error", () => {
    artHost.innerHTML = "";
    root.dataset.art = "none";
    shownArt = "";
  });
  img.src = url;
  artHost.appendChild(img);
  root.dataset.art = "present";
}

// stopTicking runs whenever the counter would be wrong: nothing playing,
// paused, or the connection down.
function stopTicking() {
  if (ticker !== null) {
    clearInterval(ticker);
    ticker = null;
  }
}

function startTicking() {
  stopTicking();
  ticker = setInterval(() => {
    if (!current || !root) {
      return;
    }
    const elapsed = Date.now() - syncedAt;
    const position = advancePosition(
      current.position_ms,
      elapsed,
      current.duration_ms
    );
    paintPosition(position, current.duration_ms);
  }, TICK_MS);
}

// setPlayingIcon swaps the toggle glyph and records which one shows, which
// picks the next tap's action.
function setPlayingIcon(playing) {
  currentlyPlaying = playing;
  if (!playPauseIconEl) return;
  const replacement = playing ? pauseIcon() : playIcon();
  playPauseIconEl.replaceWith(replacement);
  playPauseIconEl = replacement;
}

// paintPosition updates only the counter and the rim. A full rebuild every
// second is wasted work on a MediaTek SoC.
function paintPosition(positionMs, durationMs) {
  elapsedEl.textContent = formatTime(positionMs);
  setArc(arcEl, progressFraction(positionMs, durationMs));
}

export function onState(source, data) {
  if (!root) {
    return;
  }

  // A stalled feed must stop the counter.
  if (source === "connection") {
    if (data && data.state === "live" && current && current.playing !== false) {
      startTicking();
    } else {
      stopTicking();
    }
    return;
  }

  if (source !== "spotify") {
    return;
  }

  // The agent owns the layout, since the real device loads one fixed URL.
  // Applied on every reading, playing or not. Left alone when absent, which
  // keeps the query-param default for development without an agent.
  if (data && data.layout) {
    root.dataset.layout = data.layout;
  }

  if (!isPlayable(data)) {
    showIdle();
    return;
  }

  setArt(data.art_url || "");

  controlsEl.hidden = false;
  titleEl.className = "spotify-title";
  titleEl.textContent = data.title;
  artistEl.textContent = data.artist || "";
  albumEl.textContent = data.album || "";
  elapsedEl.textContent = formatTime(data.position_ms);
  totalEl.textContent = formatTime(data.duration_ms);

  // Paused restyles the face so the state reads at a glance.
  const paused = data.playing === false;
  // The reading wins over the icon a tap set locally, including changes made
  // from outside the panel.
  setPlayingIcon(!paused);
  root.classList.toggle("is-paused", paused);
  stateEl.textContent = paused ? "paused" : "";
  arcEl.classList.toggle("is-warn", paused);
  arcEl.classList.remove("is-off");

  // Resync the counter to this reading.
  current = data;
  syncedAt = Date.now();
  paintPosition(data.position_ms, data.duration_ms);

  if (paused) {
    stopTicking();
  } else {
    startTicking();
  }
}

export function teardown() {
  stopTicking();
  current = null;
  syncedAt = 0;
  root = null;
  artHost = null;
  titleEl = null;
  artistEl = null;
  albumEl = null;
  elapsedEl = null;
  totalEl = null;
  stateEl = null;
  arcEl = null;
  shownArt = "";
  controlsEl = null;
  playPauseIconEl = null;
  currentlyPlaying = false;
}

export const title = "spotify";
