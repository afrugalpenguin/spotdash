// Tests for the calendar face's pure logic.
//
// Run with: node --test agent/web/calendar.test.js

import assert from "node:assert/strict";
import { test } from "node:test";

import { urgency, formatCountdown } from "./faces/calendar.js";

test("urgency is live well before the event", () => {
  assert.equal(urgency(60), "live");
  assert.equal(urgency(16), "live");
});

test("urgency is warn inside 15 minutes", () => {
  assert.equal(urgency(15), "warn");
  assert.equal(urgency(6), "warn");
});

test("urgency is alert inside 5 minutes", () => {
  assert.equal(urgency(5), "alert");
  assert.equal(urgency(0), "alert");
});

test("a countdown of an hour or more reads in hours and minutes", () => {
  assert.equal(formatCountdown(60), "in 1h");
  assert.equal(formatCountdown(90), "in 1h 30m");
  assert.equal(formatCountdown(125), "in 2h 5m");
});

test("a countdown under an hour reads in minutes", () => {
  assert.equal(formatCountdown(45), "in 45 min");
  assert.equal(formatCountdown(1), "in 1 min");
});

test("an event that has started reads as now", () => {
  assert.equal(formatCountdown(0), "now");
  assert.equal(formatCountdown(-3), "now");
});
