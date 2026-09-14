// Clock face. Large time, small date, and a seconds arc on the rim.
//
// The rim arc is the same structural device every other face uses: the rim
// carries the quantity, the centre carries the reading.

import { createRim, RIM_CIRCUMFERENCE, setArc } from "./rim.js";

let timeEl = null;
let dateEl = null;
let arcEl = null;

export function render(container, state) {
  container.innerHTML = "";

  const rim = createRim();
  arcEl = rim.arc;
  container.appendChild(rim.svg);

  const face = document.createElement("div");
  face.className = "face";

  timeEl = document.createElement("p");
  timeEl.className = "clock-time";
  timeEl.textContent = "--:--";

  dateEl = document.createElement("p");
  dateEl.className = "clock-date";
  dateEl.textContent = "waiting for the agent";

  face.appendChild(timeEl);
  face.appendChild(dateEl);
  container.appendChild(face);

  const dot = document.createElement("div");
  dot.className = "sleep-dot";
  container.appendChild(dot);

  const existing = state && state.sources && state.sources.clock;
  if (existing && existing.data) {
    onState("clock", existing.data);
  }
}

export function onState(source, data) {
  if (source !== "clock" || !data || !timeEl) {
    return;
  }

  timeEl.textContent = data.time || "--:--";
  dateEl.textContent = data.date || "";

  // Sleep is a panel-wide state rather than a face-local one, so later faces
  // can dim without each reimplementing it.
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = data.sleep ? "true" : "false";
  }

  const seconds = typeof data.seconds === "number" ? data.seconds : 0;
  setArc(arcEl, (seconds % 60) / 60);
}

export function teardown() {
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = "false";
  }
  timeEl = null;
  dateEl = null;
  arcEl = null;
}

export const title = "clock";

// Exported for the tests and for any face that wants the same geometry.
export { RIM_CIRCUMFERENCE };
