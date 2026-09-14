// Telemetry face: four gauges on the rim, the GPU's temperature and power in
// the centre.
//
// The gauges are what you read from across the room, so they carry colour and
// nothing else competes with them. The numbers are for when you have actually
// looked.

import { createGaugeRim } from "./rim.js";

// Thresholds. Above 80 percent is worth noticing, above 95 is worth acting on,
// and a GPU above 83C is thermal throttling territory on most cards.
const WARN_PERCENT = 80;
const ALERT_PERCENT = 95;
const ALERT_TEMPERATURE_C = 83;

// gaugeSeverity classifies a percentage.
//
// A missing reading is "absent" rather than 0. An unavailable GPU that rendered
// as a calm empty gauge would be a lie, and rendering it as a red alarm would
// be a different lie.
export function gaugeSeverity(percent) {
  if (typeof percent !== "number" || Number.isNaN(percent)) {
    return "absent";
  }
  if (percent > ALERT_PERCENT) {
    return "alert";
  }
  if (percent > WARN_PERCENT) {
    return "warn";
  }
  return "ok";
}

export function gpuTemperatureSeverity(celsius) {
  if (typeof celsius !== "number" || Number.isNaN(celsius)) {
    return "absent";
  }
  return celsius > ALERT_TEMPERATURE_C ? "alert" : "ok";
}

// At 60cm on a 480px panel a decimal place is unreadable noise.
export function formatPercent(percent) {
  if (typeof percent !== "number" || Number.isNaN(percent)) {
    return "n/a";
  }
  return `${Math.round(percent)}%`;
}

export function formatPower(watts) {
  if (typeof watts !== "number" || Number.isNaN(watts)) {
    return "n/a";
  }
  return `${Math.round(watts)} W`;
}

export function formatTemperature(celsius) {
  if (typeof celsius !== "number" || Number.isNaN(celsius)) {
    return "n/a";
  }
  return `${Math.round(celsius)}°`;
}

// gaugeValues maps a telemetry reading onto the four gauges, in rim order.
//
// It never throws. A malformed message should leave gauges empty rather than
// take the face down.
export function gaugeValues(reading) {
  const safe = reading || {};
  const gpu = safe.gpu || null;
  const pick = (holder, key) => {
    const value = holder ? holder[key] : null;
    return typeof value === "number" && !Number.isNaN(value) ? value : null;
  };

  return [
    { label: "cpu", percent: pick(safe.cpu, "percent") },
    { label: "ram", percent: pick(safe.ram, "percent") },
    { label: "gpu", percent: pick(gpu, "percent") },
    { label: "vram", percent: pick(gpu, "vram_percent") },
  ];
}

// Where each gauge's readout sits, matching the quarter of the rim it belongs
// to. Percentages of the panel, so the CSS stays in one place.
const READOUT_POSITIONS = [
  { top: "27%", left: "72%" },
  { top: "73%", left: "72%" },
  { top: "73%", left: "28%" },
  { top: "27%", left: "28%" },
];

let rimGauges = [];
let readouts = [];
let temperatureEl = null;
let powerEl = null;
let noticeEl = null;

export function render(container, state) {
  container.innerHTML = "";

  const rim = createGaugeRim(4);
  rimGauges = rim.gauges;
  container.appendChild(rim.svg);

  readouts = [];
  const values = gaugeValues(null);
  values.forEach((gauge, i) => {
    const readout = document.createElement("div");
    readout.className = "gauge-readout";
    readout.style.top = READOUT_POSITIONS[i].top;
    readout.style.left = READOUT_POSITIONS[i].left;

    const name = document.createElement("span");
    name.className = "gauge-name";
    name.textContent = gauge.label;

    const value = document.createElement("span");
    value.className = "gauge-value";
    value.textContent = "n/a";

    readout.appendChild(name);
    readout.appendChild(value);
    container.appendChild(readout);
    readouts.push(value);
  });

  const centre = document.createElement("div");
  centre.className = "face telemetry-centre";

  temperatureEl = document.createElement("p");
  temperatureEl.className = "telemetry-temperature";
  temperatureEl.textContent = "n/a";

  powerEl = document.createElement("p");
  powerEl.className = "telemetry-power";
  powerEl.textContent = "";

  noticeEl = document.createElement("p");
  noticeEl.className = "telemetry-notice";
  noticeEl.textContent = "";

  centre.appendChild(temperatureEl);
  centre.appendChild(powerEl);
  centre.appendChild(noticeEl);
  container.appendChild(centre);

  const existing = state && state.sources && state.sources.telemetry;
  if (existing && existing.data) {
    onState("telemetry", existing.data);
  }
}

export function onState(source, data) {
  if (source !== "telemetry" || !data || rimGauges.length === 0) {
    return;
  }

  gaugeValues(data).forEach((gauge, i) => {
    const severity = gaugeSeverity(gauge.percent);
    const arc = rimGauges[i].arc;

    arc.classList.toggle("is-warn", severity === "warn");
    arc.classList.toggle("is-alert", severity === "alert");
    rimGauges[i].set(severity === "absent" ? 0 : gauge.percent / 100);

    readouts[i].textContent = formatPercent(gauge.percent);
    readouts[i].dataset.severity = severity;
  });

  const gpu = data.gpu || null;
  const temperature = gpu ? gpu.temperature_c : null;
  temperatureEl.textContent = formatTemperature(temperature);
  temperatureEl.dataset.severity = gpuTemperatureSeverity(temperature);
  powerEl.textContent = gpu ? formatPower(gpu.power_watts) : "";

  // A missing GPU is stated rather than left to be inferred from two gauges
  // that happen to be empty.
  noticeEl.textContent = gpu ? "" : "no gpu reading";
}

export function teardown() {
  rimGauges = [];
  readouts = [];
  temperatureEl = null;
  powerEl = null;
  noticeEl = null;
}

export const title = "telemetry";
