// Tests for the clock face's pure geometry.
//
// Run with: node --test agent/web/clock.test.js
//
// handAngles is the part worth testing hard: get the hour hand's fraction of
// travel through the current hour wrong and it visibly snaps instead of
// sweeping, on a face whose entire point is looking like a real clock.

import assert from "node:assert/strict";
import { test } from "node:test";

import { handAngles, hourNumerals, tickMarks } from "./faces/clock.js";

test("twelve o'clock exactly points every hand at the top", () => {
  const angles = handAngles("00:00", 0);

  assert.equal(angles.hour, 0);
  assert.equal(angles.minute, 0);
  assert.equal(angles.second, 0);
});

test("the second hand sweeps six degrees per second", () => {
  assert.equal(handAngles("00:00", 15).second, 90);
  assert.equal(handAngles("00:00", 30).second, 180);
  assert.equal(handAngles("00:00", 45).second, 270);
});

test("the minute hand sweeps six degrees per minute", () => {
  assert.equal(handAngles("00:15", 0).minute, 90);
  assert.equal(handAngles("00:30", 0).minute, 180);
});

test("the hour hand moves smoothly through the hour, not in jumps", () => {
  // 3:30 is half way from 3 to 4: 90 degrees plus half of one hour's 30
  // degrees, not still sitting at the 3.
  assert.equal(handAngles("03:30", 0).hour, 105);
});

test("the hour hand wraps a 24 hour reading onto a 12 hour face", () => {
  assert.equal(handAngles("15:00", 0).hour, handAngles("03:00", 0).hour);
});

test("an unparseable time reads as twelve o'clock rather than throwing", () => {
  assert.deepEqual(handAngles("", 0), { hour: 0, minute: 0, second: 0 });
  assert.deepEqual(handAngles("--:--", 0), { hour: 0, minute: 0, second: 0 });
});

test("tickMarks places sixty ticks, six degrees apart", () => {
  const ticks = tickMarks();

  assert.equal(ticks.length, 60);
  assert.equal(ticks[0].angle, 0);
  assert.equal(ticks[1].angle, 6);
  assert.equal(ticks[59].angle, 354);
});

test("tickMarks marks only the twelve hour positions as major", () => {
  const major = tickMarks()
    .filter((t) => t.major)
    .map((t) => t.angle);

  assert.deepEqual(major, [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330]);
});

test("hourNumerals labels twelve through eleven, in clock order", () => {
  const numerals = hourNumerals();

  assert.equal(numerals.length, 12);
  assert.deepEqual(
    numerals.map((n) => n.label),
    ["12", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"]
  );
  assert.deepEqual(
    numerals.map((n) => n.angle),
    [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330]
  );
});
