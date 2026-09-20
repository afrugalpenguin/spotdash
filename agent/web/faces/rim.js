// The rim: a circular track just inside the edge of the panel. Faces draw
// quantity here and detail in the centre. See docs/architecture.md, "The rim".

const SVG_NS = "http://www.w3.org/2000/svg";

export const RIM_RADIUS = 232;
export const RIM_CIRCUMFERENCE = 2 * Math.PI * RIM_RADIUS;

// createRim returns an SVG holding a full-circle track and one arc on top.
export function createRim() {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("class", "rim");
  svg.setAttribute("viewBox", "0 0 480 480");
  svg.setAttribute("aria-hidden", "true");

  const track = document.createElementNS(SVG_NS, "circle");
  track.setAttribute("class", "rim-track");
  track.setAttribute("cx", "240");
  track.setAttribute("cy", "240");
  track.setAttribute("r", String(RIM_RADIUS));

  const arc = document.createElementNS(SVG_NS, "circle");
  arc.setAttribute("class", "rim-arc");
  arc.setAttribute("cx", "240");
  arc.setAttribute("cy", "240");
  arc.setAttribute("r", String(RIM_RADIUS));
  // Start at twelve o'clock.
  arc.setAttribute("transform", "rotate(-90 240 240)");
  setArc(arc, 0);

  svg.appendChild(track);
  svg.appendChild(arc);
  return { svg, track, arc };
}

// setArc fills the given fraction of the rim, from 0 to 1.
export function setArc(arc, fraction) {
  if (!arc) {
    return;
  }
  const clamped = Math.max(0, Math.min(1, Number(fraction) || 0));
  const lit = RIM_CIRCUMFERENCE * clamped;
  arc.setAttribute("stroke-dasharray", `${lit} ${RIM_CIRCUMFERENCE - lit}`);
}

// createSegmentedRim splits the rim into count segments with a small gap
// between each, and returns the segment elements in order.
export function createSegmentedRim(count) {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("class", "rim");
  svg.setAttribute("viewBox", "0 0 480 480");
  svg.setAttribute("aria-hidden", "true");

  const track = document.createElementNS(SVG_NS, "circle");
  track.setAttribute("class", "rim-track");
  track.setAttribute("cx", "240");
  track.setAttribute("cy", "240");
  track.setAttribute("r", String(RIM_RADIUS));
  svg.appendChild(track);

  const segments = [];
  if (count <= 0) {
    return { svg, segments };
  }

  const gap = count === 1 ? 0 : 10;
  const segmentLength = RIM_CIRCUMFERENCE / count - gap;

  for (let i = 0; i < count; i += 1) {
    const segment = document.createElementNS(SVG_NS, "circle");
    segment.setAttribute("class", "rim-arc");
    segment.setAttribute("cx", "240");
    segment.setAttribute("cy", "240");
    segment.setAttribute("r", String(RIM_RADIUS));
    segment.setAttribute("transform", "rotate(-90 240 240)");
    segment.setAttribute(
      "stroke-dasharray",
      `${segmentLength} ${RIM_CIRCUMFERENCE - segmentLength}`
    );
    segment.setAttribute(
      "stroke-dashoffset",
      String(-((RIM_CIRCUMFERENCE / count) * i))
    );
    svg.appendChild(segment);
    segments.push(segment);
  }

  return { svg, segments };
}

// createGaugeRim splits the rim into count equal arcs, each with its own unlit
// track, and returns a setter per arc.
export function createGaugeRim(count) {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("class", "rim");
  svg.setAttribute("viewBox", "0 0 480 480");
  svg.setAttribute("aria-hidden", "true");

  const gauges = [];
  if (count <= 0) {
    return { svg, gauges };
  }

  // The gap keeps four gauges from reading as one ring.
  const gap = 16;
  const span = RIM_CIRCUMFERENCE / count - gap;

  for (let i = 0; i < count; i += 1) {
    const rotation = -90 + (360 / count) * i + gap / 2 / (RIM_CIRCUMFERENCE / 360);

    const track = document.createElementNS(SVG_NS, "circle");
    track.setAttribute("class", "rim-track");
    track.setAttribute("cx", "240");
    track.setAttribute("cy", "240");
    track.setAttribute("r", String(RIM_RADIUS));
    track.setAttribute("transform", `rotate(${rotation} 240 240)`);
    track.setAttribute(
      "stroke-dasharray",
      `${span} ${RIM_CIRCUMFERENCE - span}`
    );

    const arc = document.createElementNS(SVG_NS, "circle");
    arc.setAttribute("class", "rim-arc");
    arc.setAttribute("cx", "240");
    arc.setAttribute("cy", "240");
    arc.setAttribute("r", String(RIM_RADIUS));
    arc.setAttribute("transform", `rotate(${rotation} 240 240)`);
    arc.setAttribute("stroke-dasharray", `0 ${RIM_CIRCUMFERENCE}`);

    svg.appendChild(track);
    svg.appendChild(arc);

    gauges.push({
      track,
      arc,
      // set fills this gauge to the given fraction, 0 to 1.
      set(fraction) {
        const clamped = Math.max(0, Math.min(1, Number(fraction) || 0));
        const lit = span * clamped;
        arc.setAttribute(
          "stroke-dasharray",
          `${lit} ${RIM_CIRCUMFERENCE - lit}`
        );
      },
    });
  }

  return { svg, gauges };
}
