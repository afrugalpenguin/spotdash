// Tests for the clock face's pure geometry.
//
// Run with: node --test agent/web/clock.test.js
//
// handAngles is the part worth testing hard: get the hour hand's fraction of
// travel through the current hour wrong and it visibly snaps instead of
// sweeping, on a face whose entire point is looking like a real clock.

import assert from "node:assert/strict";
import { test } from "node:test";

import { handAngles, tickMarks } from "./faces/clock.js";

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

test("tickMarks places twelve ticks, thirty degrees apart", () => {
  const ticks = tickMarks();

  assert.equal(ticks.length, 12);
  assert.deepEqual(ticks.map((t) => t.angle), [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330]);
});

test("tickMarks marks twelve, three, six and nine as major", () => {
  const major = tickMarks()
    .filter((t) => t.major)
    .map((t) => t.angle);

  assert.deepEqual(major, [0, 90, 180, 270]);
});
