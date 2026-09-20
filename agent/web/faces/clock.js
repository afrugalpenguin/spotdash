// Clock face. Digital by default: time, date and a seconds arc on the rim.
// clockStyle "analogue" swaps the centre for a dial with hands.

import { createRim, RIM_CIRCUMFERENCE, RIM_RADIUS, setArc } from "./rim.js";
import { urgency, formatCountdown } from "./calendar.js";

const SVG_NS = "http://www.w3.org/2000/svg";
// Centre and radii match the rim's 480x480 viewBox and RIM_RADIUS.
const CENTER = 240;
const HAND_RADII = { hour: 120, minute: 168, second: 200 };

let timeEl = null;
let dateEl = null;
let arcEl = null;
let style = "digital";
let hourHand = null;
let minuteHand = null;
let secondHand = null;

let nextEl = null;
let nextTitleEl = null;
let nextCountdownEl = null;
let analogueNextEl = null;
let infoEl = null;

// The calendar reading, kept so paintNext can recompute the countdown on
// each clock tick. This face needs no ticker of its own.
let calendarData = null;
let calendarSyncedAt = 0;
let hideNextEvent = false;

// handAngles turns a "HH:MM" reading and a seconds count into degrees
// clockwise from twelve o'clock, one per hand. Pure, so it is tested without
// a DOM.
export function handAngles(time, seconds) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(time || "");
  if (!match) {
    return { hour: 0, minute: 0, second: 0 };
  }
  const hour = Number(match[1]) % 12;
  const minute = Number(match[2]);
  const sec = typeof seconds === "number" ? seconds % 60 : 0;

  return {
    hour: hour * 30 + minute * 0.5,
    minute: minute * 6,
    second: sec * 6,
  };
}

// tickMarks returns the sixty minute positions in degrees clockwise from
// twelve o'clock. The twelve hour positions are flagged major.
export function tickMarks() {
  const ticks = [];
  for (let i = 0; i < 60; i += 1) {
    ticks.push({ angle: i * 6, major: i % 5 === 0 });
  }
  return ticks;
}

// hourNumerals returns the twelve labels from twelve onward, each with its
// tick angle. They are drawn upright.
export function hourNumerals() {
  const numerals = [];
  for (let i = 0; i < 12; i += 1) {
    numerals.push({ label: String(i === 0 ? 12 : i), angle: i * 30 });
  }
  return numerals;
}

// compactNextEventLabel is the analogue next-up text. The gap between hub
// and numerals only fits one line.
export function compactNextEventLabel(title, minutesUntil) {
  return `${title} · ${formatCountdown(minutesUntil)}`;
}

function createTick(tick) {
  const outer = RIM_RADIUS - 6;
  const length = tick.major ? 16 : 7;
  const mark = document.createElementNS(SVG_NS, "line");
  mark.setAttribute("class", tick.major ? "clock-tick clock-tick-major" : "clock-tick");
  mark.setAttribute("x1", String(CENTER));
  mark.setAttribute("y1", String(CENTER - outer));
  mark.setAttribute("x2", String(CENTER));
  mark.setAttribute("y2", String(CENTER - outer + length));
  mark.setAttribute("transform", `rotate(${tick.angle} ${CENTER} ${CENTER})`);
  return mark;
}

// NUMERAL_RADIUS sits inside the ticks. Only the second hand reaches past it.
const NUMERAL_RADIUS = 184;

function createNumeral(numeral) {
  const rad = (numeral.angle * Math.PI) / 180;
  const text = document.createElementNS(SVG_NS, "text");
  text.setAttribute("class", "clock-numeral");
  text.setAttribute("x", String(CENTER + NUMERAL_RADIUS * Math.sin(rad)));
  text.setAttribute("y", String(CENTER - NUMERAL_RADIUS * Math.cos(rad)));
  text.setAttribute("text-anchor", "middle");
  text.setAttribute("dominant-baseline", "central");
  text.textContent = numeral.label;
  return text;
}

// A hand is a straight stroke from the centre outward. Width tells the three
// apart.
function createHand(className, length) {
  const hand = document.createElementNS(SVG_NS, "line");
  hand.setAttribute("class", className);
  hand.setAttribute("x1", "0");
  hand.setAttribute("y1", "0");
  hand.setAttribute("x2", "0");
  hand.setAttribute("y2", String(-length));
  return hand;
}

function setHand(hand, degrees) {
  if (!hand) {
    return;
  }
  // The transform list applies right to left: rotate about the hand's own
  // origin, then translate to the dial centre.
  hand.setAttribute("transform", `translate(${CENTER} ${CENTER}) rotate(${degrees})`);
}

export function render(container, state) {
  container.innerHTML = "";
  style = (state && state.clockStyle) === "analogue" ? "analogue" : "digital";
  hideNextEvent = Boolean(state && state.hideNextEvent);

  const rim = createRim();
  arcEl = style === "digital" ? rim.arc : null;
  container.appendChild(rim.svg);

  if (style === "analogue") {
    const hands = document.createElementNS(SVG_NS, "svg");
    hands.setAttribute("class", "clock-hands");
    hands.setAttribute("viewBox", "0 0 480 480");
    hands.setAttribute("aria-hidden", "true");

    for (const tick of tickMarks()) {
      hands.appendChild(createTick(tick));
    }
    for (const numeral of hourNumerals()) {
      hands.appendChild(createNumeral(numeral));
    }

    hourHand = createHand("clock-hand clock-hand-hour", HAND_RADII.hour);
    minuteHand = createHand("clock-hand clock-hand-minute", HAND_RADII.minute);
    secondHand = createHand("clock-hand clock-hand-second", HAND_RADII.second);
    hands.appendChild(hourHand);
    hands.appendChild(minuteHand);
    hands.appendChild(secondHand);

    const hub = document.createElementNS(SVG_NS, "circle");
    hub.setAttribute("class", "clock-hub");
    hub.setAttribute("cx", String(CENTER));
    hub.setAttribute("cy", String(CENTER));
    hub.setAttribute("r", "5");
    hands.appendChild(hub);

    container.appendChild(hands);

    // Not wrapped in .face, which is sized for the digital layout. This
    // block is positioned straight off #panel like .rim and #connection.
    const info = document.createElement("div");
    info.className = "clock-analogue-info";

    dateEl = document.createElement("p");
    dateEl.className = "clock-date-analogue";
    dateEl.textContent = "waiting for the agent";
    info.appendChild(dateEl);

    analogueNextEl = document.createElement("p");
    analogueNextEl.className = "clock-analogue-next";
    info.appendChild(analogueNextEl);

    container.appendChild(info);
    infoEl = info;
  } else {
    const face = document.createElement("div");
    face.className = "face";

    timeEl = document.createElement("p");
    timeEl.className = "clock-time";
    timeEl.textContent = "--:--";

    dateEl = document.createElement("p");
    dateEl.className = "clock-date";
    dateEl.textContent = "waiting for the agent";

    nextEl = document.createElement("p");
    nextEl.className = "clock-next";
    nextTitleEl = document.createElement("span");
    nextTitleEl.className = "clock-next-title";
    nextCountdownEl = document.createElement("span");
    nextCountdownEl.className = "clock-next-countdown";
    nextEl.appendChild(nextTitleEl);
    nextEl.appendChild(nextCountdownEl);

    face.appendChild(timeEl);
    face.appendChild(dateEl);
    face.appendChild(nextEl);
    container.appendChild(face);
  }

  const dot = document.createElement("div");
  dot.className = "sleep-dot";
  container.appendChild(dot);

  const existing = state && state.sources && state.sources.clock;
  if (existing && existing.data) {
    onState("clock", existing.data);
  }
  const calendar = state && state.sources && state.sources.calendar;
  if (calendar && calendar.data) {
    onState("calendar", calendar.data);
  }
}

export function onState(source, data) {
  if (source === "calendar") {
    calendarData = data && data.title ? data : null;
    calendarSyncedAt = Date.now();
    paintNext();
    return;
  }

  if (source !== "clock" || !data) {
    return;
  }

  const seconds = typeof data.seconds === "number" ? data.seconds : 0;

  if (dateEl) {
    dateEl.textContent = data.date || "";
  }

  // Sleep is panel-wide, so other faces can dim without their own logic.
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = data.sleep ? "true" : "false";
  }

  if (style === "analogue") {
    const angles = handAngles(data.time, seconds);
    setHand(hourHand, angles.hour);
    setHand(minuteHand, angles.minute);
    setHand(secondHand, angles.second);
    paintNext();
    return;
  }

  if (timeEl) {
    timeEl.textContent = data.time || "--:--";
  }
  setArc(arcEl, (seconds % 60) / 60);
  paintNext();
}

function paintNext() {
  if (style === "analogue") {
    paintAnalogueNext();
  } else {
    paintDigitalNext();
  }
}

function paintDigitalNext() {
  if (!nextEl) return;

  if (hideNextEvent || !calendarData) {
    nextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  nextEl.hidden = false;
  nextTitleEl.textContent = calendarData.title;
  nextCountdownEl.textContent = formatCountdown(minutesUntil);
  nextCountdownEl.className = `clock-next-countdown is-${urgency(minutesUntil)}`;
}

function paintAnalogueNext() {
  if (!analogueNextEl || !infoEl) return;

  if (hideNextEvent || !calendarData) {
    infoEl.classList.remove("has-next");
    analogueNextEl.hidden = true;
    return;
  }

  const elapsedMinutes = (Date.now() - calendarSyncedAt) / 60000;
  const minutesUntil = Math.round(calendarData.minutes_until - elapsedMinutes);

  infoEl.classList.add("has-next");
  analogueNextEl.hidden = false;
  analogueNextEl.textContent = compactNextEventLabel(calendarData.title, minutesUntil);
  analogueNextEl.className = `clock-analogue-next is-${urgency(minutesUntil)}`;
}

export function teardown() {
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = "false";
  }
  timeEl = null;
  dateEl = null;
  arcEl = null;
  hourHand = null;
  minuteHand = null;
  secondHand = null;
  nextEl = null;
  nextTitleEl = null;
  nextCountdownEl = null;
  analogueNextEl = null;
  infoEl = null;
  calendarData = null;
}

export const title = "clock";

// Exported for the tests and for any face that wants the same geometry.
export { RIM_CIRCUMFERENCE };
