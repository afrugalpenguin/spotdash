// Settings page: accent colour, clock style, and which faces show.
// /settings and this page's structure are generic enough that a new
// setting is an addition here, not a rewrite.
//
// FACES here has to name the same faces, by the same titles, as ALL_FACES
// in app.js and KnownFaces in the agent's config package - three places
// that have to agree, the same kind of one-line-per-thing table this
// codebase already accepts elsewhere (the source factory table, for one).
//
// clock has no row here (same as before) - there's nothing to toggle, it
// can't be hidden. Its two settings ("Analogue" and "Show next event")
// live under the Clock heading instead, built separately below.

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
// One checkbox per face, keyed by title, built once in start().
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
  // No accent_color configured: show the stylesheet's own built-in default
  // rather than an arbitrary fallback hardcoded here too, so this page and
  // the panel can never disagree about what "default" means.
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
    // Same rule the agent enforces; caught here first so it never has to
    // make a round trip to find out.
    setStatus("At least one face has to stay on.", "error");
    return;
  }

  saveButton.disabled = true;
  setStatus("Saving…", "");
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
  // Same handling as the panel itself: the token travels once in the URL,
  // is exchanged for a session cookie by this very page load, and is then
  // stripped from the visible address bar.
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
