// Settings page: today just the accent colour, but /settings/accent and this
// page's structure are generic enough that a second field is an addition
// here, not a rewrite.

import { readToken } from "./app.js";

let accentInput = null;
let saveButton = null;
let statusEl = null;

function setStatus(text, state) {
  if (!statusEl) return;
  statusEl.textContent = text;
  if (state) {
    statusEl.dataset.state = state;
  } else {
    delete statusEl.dataset.state;
  }
}

async function loadCurrent() {
  try {
    const response = await fetch("/settings/accent");
    if (!response.ok) {
      throw new Error(`server returned ${response.status}`);
    }
    const data = await response.json();
    if (data.accent_color) {
      accentInput.value = data.accent_color;
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

async function save() {
  saveButton.disabled = true;
  setStatus("Saving…", "");
  try {
    const response = await fetch("/settings/accent", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ accent_color: accentInput.value }),
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

  saveButton.addEventListener("click", save);
  loadCurrent();
}
