// Tests for the telemetry face's pure logic.
//
// Run with: node --test agent/web/telemetry.test.js
//
// The thresholds are the part worth testing hard: a gauge that stays green
// while a card sits at 97 percent is worse than no gauge at all.

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  gaugeSeverity,
  gpuTemperatureSeverity,
  formatPercent,
  formatPower,
  formatTemperature,
  gaugeValues,
} from "./faces/telemetry.js";

test("a gauge is ok up to and including 80 percent", () => {
  assert.equal(gaugeSeverity(0), "ok");
  assert.equal(gaugeSeverity(50), "ok");
  assert.equal(gaugeSeverity(80), "ok");
});

test("a gauge is amber above 80 percent", () => {
  assert.equal(gaugeSeverity(80.1), "warn");
  assert.equal(gaugeSeverity(90), "warn");
  assert.equal(gaugeSeverity(95), "warn");
});

test("a gauge is red above 95 percent", () => {
  assert.equal(gaugeSeverity(95.1), "alert");
  assert.equal(gaugeSeverity(99), "alert");
  assert.equal(gaugeSeverity(100), "alert");
});

test("a gauge with no reading is neither ok nor alarming", () => {
  // An unavailable GPU must not render as a calm zero, and must not render as
  // a red alarm either. It is absent, and it should look absent.
  assert.equal(gaugeSeverity(null), "absent");
  assert.equal(gaugeSeverity(undefined), "absent");
  assert.equal(gaugeSeverity(Number.NaN), "absent");
});

test("gpu temperature is red above 83C", () => {
  assert.equal(gpuTemperatureSeverity(30), "ok");
  assert.equal(gpuTemperatureSeverity(83), "ok");
  assert.equal(gpuTemperatureSeverity(83.1), "alert");
  assert.equal(gpuTemperatureSeverity(91), "alert");
});

test("gpu temperature with no reading is absent", () => {
  assert.equal(gpuTemperatureSeverity(null), "absent");
  assert.equal(gpuTemperatureSeverity(undefined), "absent");
});

test("percentages round to whole numbers for the panel", () => {
  // At 60cm on a 480px display a decimal place is unreadable noise.
  assert.equal(formatPercent(3.757693553611921), "4%");
  assert.equal(formatPercent(51.47493560754183), "51%");
  assert.equal(formatPercent(100), "100%");
});

test("a missing percentage reads as unavailable, not as zero", () => {
  assert.equal(formatPercent(null), "n/a");
  assert.equal(formatPercent(undefined), "n/a");
});

test("power and temperature carry their units", () => {
  assert.equal(formatPower(22.121), "22 W");
  assert.equal(formatPower(120.5), "121 W");
  assert.equal(formatTemperature(30), "30°");
  assert.equal(formatPower(null), "n/a");
  assert.equal(formatTemperature(null), "n/a");
});

test("gaugeValues maps a full reading onto the four gauges", () => {
  const reading = {
    cpu: { percent: 12.5, per_core: [10, 15] },
    ram: { percent: 51.4, used_bytes: 1, total_bytes: 2 },
    disks: [],
    gpu: { percent: 17, vram_percent: 21.8, temperature_c: 30, power_watts: 21.84 },
  };

  const gauges = gaugeValues(reading);

  assert.deepEqual(
    gauges.map((g) => g.label),
    ["cpu", "ram", "gpu", "vram"]
  );
  assert.equal(gauges[0].percent, 12.5);
  assert.equal(gauges[1].percent, 51.4);
  assert.equal(gauges[2].percent, 17);
  assert.equal(gauges[3].percent, 21.8);
});

test("gaugeValues leaves the gpu gauges empty when there is no gpu", () => {
  // The degraded payload from the agent. The two GPU gauges have to read as
  // absent rather than as a real zero.
  const reading = {
    cpu: { percent: 12.5, per_core: [] },
    ram: { percent: 51.4 },
    disks: [],
    gpu: null,
  };

  const gauges = gaugeValues(reading);

  assert.equal(gauges[0].percent, 12.5);
  assert.equal(gauges[2].percent, null);
  assert.equal(gauges[3].percent, null);
  assert.equal(gaugeSeverity(gauges[2].percent), "absent");
});

test("gaugeValues survives a reading with missing sections", () => {
  // Never throw on the panel. A malformed message should degrade to unknown
  // values, not take the face down.
  const gauges = gaugeValues({});

  assert.equal(gauges.length, 4);
  for (const gauge of gauges) {
    assert.equal(gauge.percent, null);
  }
});
