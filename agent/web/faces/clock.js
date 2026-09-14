// Clock face. Digital by default: large time, small date, and a seconds arc
// on the rim. clockStyle "analogue" on the panel state swaps the centre for
// a drawn clock face with hour/minute/second hands instead, still on the
// same rim.
//
// The rim arc is the same structural device every other face uses: the rim
// carries the quantity, the centre carries the reading.

import { createRim, RIM_CIRCUMFERENCE, RIM_RADIUS, setArc } from "./rim.js";

const SVG_NS = "http://www.w3.org/2000/svg";
// Centre and radius match the rim's own 480x480 viewBox and RIM_RADIUS, so
// the hands read as pointing at the rim's ticks rather than floating free of
// them at a different scale.
const CENTER = 240;
const HAND_RADII = { hour: 120, minute: 168, second: 184 };

let timeEl = null;
let dateEl = null;
let arcEl = null;
let style = "digital";
let hourHand = null;
let minuteHand = null;
let secondHand = null;

// handAngles turns a "HH:MM" reading and a seconds count into degrees of
// clockwise rotation from twelve o'clock, one per hand. Pure so the sweep
// math can be tested without a DOM: get the hour hand's fraction through the
// current hour wrong and it visibly snaps instead of sweeping.
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

// tickMarks returns all sixty minute positions as degrees clockwise from
// twelve o'clock, each flagged major at the twelve hour positions (drawn
// longer and bolder, the same weight distinction a real watch face uses).
// Sixty rather than twelve reads as an instrument face rather than a plain
// dial - it is what the numerals below sit inside of.
export function tickMarks() {
  const ticks = [];
  for (let i = 0; i < 60; i += 1) {
    ticks.push({ angle: i * 6, major: i % 5 === 0 });
  }
  return ticks;
}

// hourNumerals returns the twelve hour labels in clock order, starting at
// twelve, each with the angle its tick sits at. Rendered upright rather than
// rotated with the tick: a clock's numerals always read right-way-up, only
// their position goes around the dial.
export function hourNumerals() {
  const numerals = [];
  for (let i = 0; i < 12; i += 1) {
    numerals.push({ label: String(i === 0 ? 12 : i), angle: i * 30 });
  }
  return numerals;
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

// NUMERAL_RADIUS sits inside the ticks, clear of the minute hand's own
// length, so nothing on the dial ever overlaps.
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

// A hand is a plain straight stroke with a rounded tip, running only from
// the centre outward - no back-tail, no taper. Width is what tells hour
// from minute from second apart, the same as a real clock's hands.
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
  // Rotate first (the hand's points are defined around its own origin),
  // then move that origin to the dial's centre: composed in that order so
  // the rotation happens in hand-local space rather than around the corner
  // of the viewBox.
  hand.setAttribute("transform", `translate(${CENTER} ${CENTER}) rotate(${degrees})`);
}

export function render(container, state) {
  container.innerHTML = "";
  style = (state && state.clockStyle) === "analogue" ? "analogue" : "digital";

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

    // Not wrapped in .face: .face is positioned and sized for the digital
    // layout's centred text block, but this date sits low in the circle,
    // clear of the hands, the same way .rim and #connection are positioned
    // straight off #panel rather than through .face.
    dateEl = document.createElement("p");
    dateEl.className = "clock-date clock-date-analogue";
    dateEl.textContent = "waiting for the agent";
    container.appendChild(dateEl);
  } else {
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
  }

  const dot = document.createElement("div");
  dot.className = "sleep-dot";
  container.appendChild(dot);

  const existing = state && state.sources && state.sources.clock;
  if (existing && existing.data) {
    onState("clock", existing.data);
  }
}

export function onState(source, data) {
  if (source !== "clock" || !data) {
    return;
  }

  const seconds = typeof data.seconds === "number" ? data.seconds : 0;

  if (dateEl) {
    dateEl.textContent = data.date || "";
  }

  // Sleep is a panel-wide state rather than a face-local one, so later faces
  // can dim without each reimplementing it.
  const panel = document.getElementById("panel");
  if (panel) {
    panel.dataset.sleep = data.sleep ? "true" : "false";
  }

  if (style === "analogue") {
    const angles = handAngles(data.time, seconds);
    setHand(hourHand, angles.hour);
    setHand(minuteHand, angles.minute);
    setHand(secondHand, angles.second);
    return;
  }

  if (timeEl) {
    timeEl.textContent = data.time || "--:--";
  }
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
  hourHand = null;
  minuteHand = null;
  secondHand = null;
}

export const title = "clock";

// Exported for the tests and for any face that wants the same geometry.
export { RIM_CIRCUMFERENCE };
