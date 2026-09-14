// Status face. The debug view, and the fallback whenever the socket is down.
//
// This face has to be truthful when everything else is broken, so it reads only
// from state it already holds and never assumes a live connection.

import { createSegmentedRim } from "./rim.js";

let root = null;
let listEl = null;
let errorEl = null;
let footerEl = null;
let segments = [];
let rimHost = null;
let currentState = null;

export function render(container, state) {
  container.innerHTML = "";
  currentState = state;

  rimHost = document.createElement("div");
  rimHost.className = "rim";
  container.appendChild(rimHost);

  root = document.createElement("div");
  root.className = "face status";

  listEl = document.createElement("ul");
  listEl.className = "status-list";

  errorEl = document.createElement("p");
  errorEl.className = "status-error";

  footerEl = document.createElement("div");
  footerEl.className = "status-footer";

  root.appendChild(listEl);
  root.appendChild(errorEl);
  root.appendChild(footerEl);
  container.appendChild(root);

  update();
}

// The manager mutates its state object in place, so the reference handed to
// render stays current and onState only has to trigger a redraw.
export function onState(_source, _data) {
  update();
}

function update() {
  if (!listEl || !currentState) {
    return;
  }

  const names = Object.keys(currentState.sources || {}).sort();
  drawRim(names);
  drawList(names);
  drawFirstError(names);
  drawFooter();
}

function drawRim(names) {
  // The rim is rebuilt only when the number of sources changes, which is once
  // in practice. A weak SoC should not redraw SVG every second.
  if (segments.length !== names.length) {
    rimHost.innerHTML = "";
    const rim = createSegmentedRim(names.length);
    segments = rim.segments;
    rimHost.appendChild(rim.svg);
  }

  names.forEach((name, i) => {
    const segment = segments[i];
    if (!segment) {
      return;
    }
    const status = (currentState.sources[name] || {}).status || "unknown";
    segment.classList.toggle("is-warn", status === "degraded");
    segment.classList.toggle("is-alert", false);
    segment.classList.toggle(
      "is-off",
      status === "disabled" || status === "unknown"
    );
  });
}

function drawList(names) {
  listEl.innerHTML = "";

  if (names.length === 0) {
    const empty = document.createElement("li");
    empty.className = "status-empty";
    empty.textContent = "no sources reported yet";
    listEl.appendChild(empty);
    return;
  }

  for (const name of names) {
    const source = currentState.sources[name] || {};
    const row = document.createElement("li");
    row.className = "status-row";

    const nameEl = document.createElement("span");
    nameEl.className = "status-name";
    nameEl.textContent = name;

    const valueEl = document.createElement("span");
    valueEl.className = "status-value";
    const status = source.status || "unknown";
    valueEl.dataset.status = status;
    valueEl.textContent = status;

    const ageEl = document.createElement("span");
    ageEl.className = "status-age";
    ageEl.textContent = relativeAge(source.lastUpdate);

    row.appendChild(nameEl);
    row.appendChild(valueEl);
    row.appendChild(ageEl);
    listEl.appendChild(row);
  }
}

function drawFirstError(names) {
  // One error at a time. The panel is 480px across and a wall of text at that
  // size is unreadable; the rest are in the agent log.
  const withError = names.find(
    (name) => (currentState.sources[name] || {}).lastError
  );
  if (!withError) {
    errorEl.textContent = "";
    return;
  }
  errorEl.textContent = `${withError}: ${currentState.sources[withError].lastError}`;
}

function drawFooter() {
  footerEl.innerHTML = "";

  const uptime = document.createElement("span");
  uptime.textContent = "up ";
  const uptimeValue = document.createElement("strong");
  uptimeValue.textContent = formatDuration(currentState.uptimeSeconds);
  uptime.appendChild(uptimeValue);

  const link = document.createElement("span");
  link.textContent = "link ";
  const linkValue = document.createElement("strong");
  linkValue.textContent = currentState.connection || "down";
  link.appendChild(linkValue);

  footerEl.appendChild(uptime);
  footerEl.appendChild(link);
}

export function relativeAge(iso) {
  if (!iso) {
    return "never";
  }
  const then = Date.parse(iso);
  if (Number.isNaN(then)) {
    return "never";
  }
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m`;
  }
  return `${Math.floor(seconds / 3600)}h`;
}

export function formatDuration(seconds) {
  const total = Math.max(0, Math.floor(Number(seconds) || 0));
  if (total === 0) {
    // Zero means /health has not answered. Reporting "0s" would read as a
    // restart that did not happen.
    return "unknown";
  }
  if (total < 60) {
    return `${total}s`;
  }
  if (total < 3600) {
    return `${Math.floor(total / 60)}m`;
  }
  if (total < 86400) {
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
  }
  return `${Math.floor(total / 86400)}d`;
}

export function teardown() {
  root = null;
  listEl = null;
  errorEl = null;
  footerEl = null;
  segments = [];
  rimHost = null;
}

export const title = "status";
