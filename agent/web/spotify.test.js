// Tests for the spotify face's pure logic.
//
// Run with: node --test agent/web/spotify.test.js

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  advancePosition,
  formatTime,
  progressFraction,
  isPlayable,
} from "./faces/spotify.js";

test("a track position formats as minutes and seconds", () => {
  assert.equal(formatTime(0), "0:00");
  assert.equal(formatTime(9000), "0:09");
  assert.equal(formatTime(115000), "1:55");
  assert.equal(formatTime(342000), "5:42");
});

test("a missing or negative position reads as the start", () => {
  assert.equal(formatTime(null), "0:00");
  assert.equal(formatTime(undefined), "0:00");
  assert.equal(formatTime(-5000), "0:00");
});

test("progress is the fraction of the track elapsed", () => {
  assert.equal(progressFraction(0, 342000), 0);
  assert.equal(progressFraction(171000, 342000), 0.5);
  assert.equal(progressFraction(342000, 342000), 1);
});

test("progress copes with a missing or zero duration", () => {
  // A zero duration would be a division by zero, and NaN passed to the rim
  // silently draws nothing.
  assert.equal(progressFraction(1000, 0), 0);
  assert.equal(progressFraction(1000, null), 0);
  assert.equal(progressFraction(null, 342000), 0);
});

test("progress never runs past the end of the rim", () => {
  assert.equal(progressFraction(400000, 342000), 1);
  assert.equal(progressFraction(-1000, 342000), 0);
});

test("a reading with no title is not playable", () => {
  assert.equal(isPlayable(null), false);
  assert.equal(isPlayable({}), false);
  assert.equal(isPlayable({ title: "" }), false);
  assert.equal(isPlayable({ title: "Peacefield - Live from Mexico City" }), true);
});

test("the position advances by the time elapsed", () => {
  // The agent sends a position once a second. Between those, the face carries
  // the counter itself, so it ticks rather than stepping.
  assert.equal(advancePosition(115000, 1000, 342000), 116000);
  assert.equal(advancePosition(115000, 8000, 342000), 123000);
});

test("the position loops back to the start at the end of the track", () => {
  assert.equal(advancePosition(341500, 1000, 342000), 500);
  assert.equal(advancePosition(342000, 0, 342000), 0);
});

test("advancing copes with a missing duration", () => {
  assert.equal(advancePosition(1000, 1000, 0), 0);
  assert.equal(advancePosition(1000, 1000, null), 0);
});

test("advancing never returns a negative position", () => {
  assert.equal(advancePosition(0, -5000, 342000), 0);
  assert.equal(advancePosition(null, 1000, 342000), 1000);
});
