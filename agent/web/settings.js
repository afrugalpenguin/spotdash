// Settings page: accent colour, clock style, and which faces show.
// FACES must match ALL_FACES in app.js and KnownFaces in the agent's config.
// clock has no row because it cannot be hidden.

import { readToken } from "./app.js";

const FACES = [
  { title: "calendar", label: "Calendar" },
  { title: "spotify", label: "Spotify" },
  { title: "telemetry", label: "Telemetry" },
  { title: "status", label: "Status" },
];

let accentInput = null;
let analogueInput = null;
let nextEventInput = null;
let saveButton = null;
let statusEl = null;
let clockEl = null;
let facesEl = null;
// One checkbox per face, keyed by title.
const toggles = new Map();

function setStatus(text, state) {
  if (!statusEl) return;
  statusEl.textContent = text;
  if (state) {
    statusEl.dataset.state = state;
  } else {
    delete statusEl.dataset.state;
  }
}

// buildToggle appends one labelled slider switch to container and returns
// its checkbox input.
function buildToggle(container, id, label) {
  const row = document.createElement("div");
  row.className = "field";

  const labelEl = document.createElement("label");
  labelEl.setAttribute("for", id);
  labelEl.textContent = label;

  const toggle = document.createElement("span");
  toggle.className = "toggle";

  const input = document.createElement("input");
  input.type = "checkbox";
  input.id = id;
  input.checked = true; // corrected once the current settings load

  const track = document.createElement("span");
  track.className = "toggle-track";

  toggle.appendChild(input);
  toggle.appendChild(track);
  row.appendChild(labelEl);
  row.appendChild(toggle);
  container.appendChild(row);

  return input;
}

function buildFaceToggles() {
  for (const face of FACES) {
    const input = buildToggle(facesEl, `face-${face.title}`, face.label);
    toggles.set(face.title, input);
  }
}

async function loadCurrent() {
  try {
    const response = await fetch("/settings");
    if (!response.ok) {
      throw new Error(`server returned ${response.status}`);
    }
    const data = await response.json();
    if (data.accent_color) {
      accentInput.value = data.accent_color;
    }
    const hidden = new Set(data.hidden_faces || []);
    for (const [title, input] of toggles) {
      input.checked = !hidden.has(title);
    }
    analogueInput.checked = data.clock_style === "analogue";
    nextEventInput.checked = !data.hide_next_event;
    if (data.accent_color) {
      return;
    }
  } catch (err) {
    setStatus("Could not load the current settings.", "error");
  }
  // No accent_color configured: show the stylesheet's default, so this page
  // and the panel agree.
  const fallback = getComputedStyle(document.documentElement)
    .getPropertyValue("--live")
    .trim();
  if (fallback) {
    accentInput.value = fallback;
  }
}

function hiddenFaceTitles() {
  return FACES.filter((face) => !toggles.get(face.title).checked).map((face) => face.title);
}

async function save() {
  const hidden = hiddenFaceTitles();
  if (hidden.length === FACES.length) {
    // The agent enforces this too. Catch it here to skip the round trip.
    setStatus("At least one face has to stay on.", "error");
    return;
  }

  saveButton.disabled = true;
  setStatus("Saving...", "");
  try {
    const response = await fetch("/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        accent_color: accentInput.value,
        hidden_faces: hidden,
        clock_style: analogueInput.checked ? "analogue" : "digital",
        hide_next_event: !nextEventInput.checked,
      }),
    });
    if (!response.ok) {
      const message = await response.text();
      throw new Error(message || `server returned ${response.status}`);
    }
    setStatus("Saved. The panel picks it up within a couple of seconds.", "ok");
  } catch (err) {
    setStatus(
      `Could not save: ${err && err.message ? err.message : err}`,
      "error"
    );
  } finally {
    saveButton.disabled = false;
  }
}

export function start() {
  // As on the panel, the token is stripped from the address bar. This page
  // load exchanges it for a session cookie.
  readToken(window.location, window.history);

  accentInput = document.getElementById("accent");
  saveButton = document.getElementById("save");
  statusEl = document.getElementById("status");
  clockEl = document.getElementById("clock-settings");
  facesEl = document.getElementById("faces");

  buildFaceToggles();
  analogueInput = buildToggle(clockEl, "clock-analogue", "Analogue");
  nextEventInput = buildToggle(clockEl, "clock-next-event", "Show next event");
  saveButton.addEventListener("click", save);
  loadCurrent();
}
