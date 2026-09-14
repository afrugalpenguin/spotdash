// Spotify face: what is playing, with the cover behind it and the track
// position on the rim.
//
// Two layouts, because which one wins is a decision about the room rather than
// the screen. The cover as a disc keeps the type on flat black and is the more
// legible of the two at 60cm; the cover filling the panel has more presence and
// depends on the sleeve being dark where the type sits. Choose with
// ?layout=fill on the panel URL.

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

// The agent polls Spotify rather than being pushed to, and it cannot poll every
// second without being rate limited, so readings arrive every few seconds. The
// face carries the counter in between and resyncs whenever one lands.
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

// advancePosition moves the counter on by the time elapsed, looping back to the
// start at the end of the track.
//
// The agent sends a position roughly once a second. Carrying the counter
// between those messages is what makes it tick rather than step, and it means a
// brief stall in the feed does not look like a frozen panel.
export function advancePosition(positionMs, elapsedMs, durationMs) {
  if (typeof durationMs !== "number" || !Number.isFinite(durationMs) || durationMs <= 0) {
    return 0;
  }
  const from = typeof positionMs === "number" && Number.isFinite(positionMs) ? positionMs : 0;
  const by = typeof elapsedMs === "number" && Number.isFinite(elapsedMs) ? elapsedMs : 0;
  const next = (from + by) % durationMs;
  return next < 0 ? 0 : next;
}

function layoutFromQuery(search) {
  return new URLSearchParams(search || "").get("layout") === "fill"
    ? "fill"
    : "disc";
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

// setArt swaps the cover only when it actually changes. A 480px image decoded
// every second on a MediaTek SoC is real work for no benefit.
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
  // A cover that fails to load leaves the type on black, which the layout is
  // designed to survive, rather than a broken image icon.
  img.addEventListener("error", () => {
    artHost.innerHTML = "";
    root.dataset.art = "none";
    shownArt = "";
  });
  img.src = url;
  artHost.appendChild(img);
  root.dataset.art = "present";
}

// stopTicking is called whenever the counter would be lying: nothing playing,
// playback paused, or the connection down. A counter that keeps climbing on a
// dead feed is worse than one that visibly stops.
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

// paintPosition updates only the counter and the rim, which is all that changes
// between readings. Rebuilding the face every second on a MediaTek SoC would be
// wasteful for no visible difference.
function paintPosition(positionMs, durationMs) {
  elapsedEl.textContent = formatTime(positionMs);
  setArc(arcEl, progressFraction(positionMs, durationMs));
}

export function onState(source, data) {
  if (!root) {
    return;
  }

  // The panel tells every face when the link changes. A stalled feed must stop
  // the counter rather than let it run away from the truth.
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

  if (!isPlayable(data)) {
    showIdle();
    return;
  }

  setArt(data.art_url || "");

  titleEl.className = "spotify-title";
  titleEl.textContent = data.title;
  artistEl.textContent = data.artist || "";
  albumEl.textContent = data.album || "";
  elapsedEl.textContent = formatTime(data.position_ms);
  totalEl.textContent = formatTime(data.duration_ms);

  // Paused changes form rather than only adding a word, so the answer to "is it
  // playing" arrives before anything is read.
  const paused = data.playing === false;
  root.classList.toggle("is-paused", paused);
  stateEl.textContent = paused ? "paused" : "";
  arcEl.classList.toggle("is-warn", paused);
  arcEl.classList.remove("is-off");

  // Resync: this reading is the truth, and the counter carries on from here
  // until the next one arrives.
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
}

export const title = "spotify";
