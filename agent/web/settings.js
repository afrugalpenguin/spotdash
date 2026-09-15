// Settings page: accent colour and which faces show. /settings and this
// page's structure are generic enough that a third setting is an addition
// here, not a rewrite.
//
// FACES here has to name the same faces, by the same titles, as ALL_FACES
// in app.js and KnownFaces in the agent's config package - three places
// that have to agree, the same kind of one-line-per-thing table this
// codebase already accepts elsewhere (the source factory table, for one).

import { readToken } from "./app.js";

const FACES = [
  { title: "overview", label: "Overview" },
  { title: "clock", label: "Clock" },
  { title: "calendar", label: "Calendar" },
  { title: "spotify", label: "Spotify" },
  { title: "telemetry", label: "Telemetry" },
  { title: "status", label: "Status" },
];

let accentInput = null;
let saveButton = null;
let statusEl = null;
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

function buildFaceToggles() {
  for (const face of FACES) {
    const row = document.createElement("div");
    row.className = "field";

    const label = document.createElement("label");
    label.setAttribute("for", `face-${face.title}`);
    label.textContent = face.label;

    const toggle = document.createElement("span");
    toggle.className = "toggle";

    const input = document.createElement("input");
    input.type = "checkbox";
    input.id = `face-${face.title}`;
    input.checked = true; // visible by default, corrected once the current settings load

    const track = document.createElement("span");
    track.className = "toggle-track";

    toggle.appendChild(input);
    toggle.appendChild(track);
    row.appendChild(label);
    row.appendChild(toggle);
    facesEl.appendChild(row);

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
  facesEl = document.getElementById("faces");

  buildFaceToggles();
  saveButton.addEventListener("click", save);
  loadCurrent();
}
