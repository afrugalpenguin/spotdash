// Tests for the panel logic that does not touch the DOM.
//
// Run with: node --test agent/web
//
// The DOM-building parts are reviewed in dev.html, which renders every face
// against every state. These are the parts where a silent bug would leave the
// panel looking fine and being wrong.

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  applyHealth,
  applyMessage,
  faceIndexFromQuery,
  isStale,
  jittered,
  nextBackoff,
  readToken,
  state,
  visibleFaces,
} from "./app.js";
import { formatDuration, relativeAge } from "./faces/status.js";
import { RIM_CIRCUMFERENCE } from "./faces/rim.js";

function resetState() {
  state.sources = {};
  state.uptimeSeconds = 0;
  state.version = "";
  state.accentColor = "";
  state.hiddenFaces = [];
  state.clockStyle = "digital";
  state.connection = "connecting";
  state.lastError = "";
  state.clockOffsetMs = 0;
}

// fakeHistory records what readToken rewrites the URL to.
function fakeHistory() {
  const calls = [];
  return {
    calls,
    replaceState(_stateObj, _title, url) {
      calls.push(url);
    },
  };
}

test("readToken takes the token out of the URL", () => {
  const history = fakeHistory();
  const location = { href: "http://panel.local/?token=abc123&face=clock" };

  const token = readToken(location, history);

  assert.equal(token, "abc123");
  assert.equal(history.calls.length, 1);
  assert.ok(
    !history.calls[0].includes("abc123"),
    `rewritten URL still contains the token: ${history.calls[0]}`
  );
  assert.ok(
    history.calls[0].includes("face=clock"),
    "other query parameters should survive"
  );
});

test("readToken returns empty and rewrites nothing when there is no token", () => {
  const history = fakeHistory();

  const token = readToken({ href: "http://panel.local/?face=status" }, history);

  assert.equal(token, "");
  assert.equal(history.calls.length, 0);
});

test("faceIndexFromQuery selects the requested face", () => {
  const names = ["clock", "status"];

  assert.equal(faceIndexFromQuery("?face=status", names), 1);
  assert.equal(faceIndexFromQuery("?face=clock", names), 0);
});

test("faceIndexFromQuery falls back to the first face", () => {
  const names = ["clock", "status"];

  assert.equal(faceIndexFromQuery("", names), 0);
  assert.equal(faceIndexFromQuery("?face=spotify", names), 0);
});

test("backoff doubles and settles at the ceiling", () => {
  let delay = 500;
  const seen = [delay];
  for (let i = 0; i < 10; i += 1) {
    delay = nextBackoff(delay);
    seen.push(delay);
  }

  assert.ok(seen[1] > seen[0], "backoff should grow");
  assert.equal(delay, 15000, "backoff should settle at the ceiling");
  assert.ok(
    seen.every((value) => value <= 15000),
    "backoff must never exceed the ceiling"
  );
});

test("jitter stays within half the delay and never exceeds it", () => {
  for (let i = 0; i < 200; i += 1) {
    const value = jittered(1000);
    assert.ok(value >= 500 && value <= 1000, `jittered value out of range: ${value}`);
  }
});

test("applyMessage stores the reading against its source", () => {
  resetState();

  applyMessage({
    source: "clock",
    ts: "2026-09-14T10:00:00Z",
    data: { time: "10:00" },
  });

  assert.equal(state.sources.clock.data.time, "10:00");
  assert.equal(state.sources.clock.lastUpdate, "2026-09-14T10:00:00Z");
});

test("applyMessage ignores a message with no source", () => {
  resetState();

  applyMessage({ ts: "2026-09-14T10:00:00Z", data: {} });
  applyMessage(null);

  assert.deepEqual(state.sources, {});
});

test("applyHealth keeps the reading a source already had", () => {
  // Data comes from the socket and status comes from /health. A health poll
  // must not wipe the reading the panel is currently showing.
  resetState();
  applyMessage({ source: "clock", ts: "2026-09-14T10:00:00Z", data: { time: "10:00" } });

  applyHealth({
    version: "1.2.3",
    uptime_seconds: 42,
    sources: { clock: { status: "ok", last_update: "2026-09-14T10:00:01Z" } },
  });

  assert.equal(state.sources.clock.data.time, "10:00");
  assert.equal(state.sources.clock.status, "ok");
  assert.equal(state.uptimeSeconds, 42);
  assert.equal(state.version, "1.2.3");
});

test("applyHealth carries the configured accent colour", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1, accent_color: "#7c83fd" });

  assert.equal(state.accentColor, "#7c83fd");
});

test("applyHealth leaves accentColor empty when unconfigured", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1 });

  assert.equal(state.accentColor, "");
});

test("applyHealth carries the configured hidden faces", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1, hidden_faces: ["clock", "telemetry"] });

  assert.deepEqual(state.hiddenFaces, ["clock", "telemetry"]);
});

test("applyHealth leaves hiddenFaces empty when unconfigured", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1 });

  assert.deepEqual(state.hiddenFaces, []);
});

test("applyHealth carries the configured clock style", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1, clock_style: "analogue" });

  assert.equal(state.clockStyle, "analogue");
});

test("applyHealth defaults clockStyle to digital when unconfigured", () => {
  resetState();

  applyHealth({ version: "1.2.3", uptime_seconds: 1 });

  assert.equal(state.clockStyle, "digital");
});

test("visibleFaces filters out the hidden titles", () => {
  const faces = [{ title: "a" }, { title: "b" }, { title: "c" }];

  const got = visibleFaces(faces, ["b"]);

  assert.deepEqual(got.map((f) => f.title), ["a", "c"]);
});

test("visibleFaces keeps everything when nothing is hidden", () => {
  const faces = [{ title: "a" }, { title: "b" }];

  const got = visibleFaces(faces, []);

  assert.deepEqual(got.map((f) => f.title), ["a", "b"]);
});

test("visibleFaces falls back to every face rather than returning none", () => {
  const faces = [{ title: "a" }, { title: "b" }];

  const got = visibleFaces(faces, ["a", "b"]);

  assert.deepEqual(got.map((f) => f.title), ["a", "b"]);
});

test("applyHealth carries the last error through", () => {
  resetState();

  applyHealth({
    uptime_seconds: 10,
    sources: {
      telemetry: {
        status: "degraded",
        last_update: "2026-09-14T10:00:00Z",
        last_error: "nvml: could not load nvml.dll",
      },
    },
  });

  assert.equal(state.sources.telemetry.status, "degraded");
  assert.equal(
    state.sources.telemetry.lastError,
    "nvml: could not load nvml.dll"
  );
});

test("applyHealth clears an error once a source recovers", () => {
  resetState();
  applyHealth({
    uptime_seconds: 10,
    sources: { telemetry: { status: "degraded", last_error: "boom" } },
  });

  applyHealth({
    uptime_seconds: 20,
    sources: { telemetry: { status: "ok", last_update: "2026-09-14T10:00:00Z" } },
  });

  assert.equal(state.sources.telemetry.lastError, "");
});

test("uptime of zero reads as unknown rather than a restart", () => {
  // Zero means /health has not answered. Showing "0s" would say the agent
  // just restarted, which is a different and wrong conclusion.
  assert.equal(formatDuration(0), "unknown");
  assert.equal(formatDuration(undefined), "unknown");
});

test("uptime formats at each scale", () => {
  assert.equal(formatDuration(45), "45s");
  assert.equal(formatDuration(90), "1m");
  assert.equal(formatDuration(3600), "1h");
  assert.equal(formatDuration(3660), "1h 1m");
  assert.equal(formatDuration(90000), "1d");
});

test("a source that never updated reads as never, not as just now", () => {
  assert.equal(relativeAge(""), "never");
  assert.equal(relativeAge(undefined), "never");
  assert.equal(relativeAge("not a timestamp"), "never");
});

test("relative age counts up from the timestamp", () => {
  const twoMinutesAgo = new Date(Date.now() - 125000).toISOString();

  assert.equal(relativeAge(twoMinutesAgo), "2m");
});

test("a connection is not stale before the threshold has passed", () => {
  assert.equal(isStale(1000, 1000 + 59999, 60000), false);
});

test("a connection is stale once the threshold has passed", () => {
  assert.equal(isStale(1000, 1000 + 60001, 60000), true);
});

test("a connection that has never received anything is not stale", () => {
  // 0 means "never connected", a real attempt already handles that; the
  // watchdog is not the thing that chases an initial connection.
  assert.equal(isStale(0, 999999999, 60000), false);
});

test("the rim geometry matches the drawn radius", () => {
  assert.ok(
    Math.abs(RIM_CIRCUMFERENCE - 2 * Math.PI * 232) < 0.001,
    "rim circumference must match the radius the SVG is drawn with"
  );
});

test("applyHealth works out how far the device clock is from the agent's", () => {
  resetState();
  const agentNow = "2026-09-14T10:00:00Z";
  const deviceIsAnHourAhead = Date.parse(agentNow) + 3600000;

  applyHealth({ now: agentNow, sources: {} }, deviceIsAnHourAhead);

  assert.equal(state.clockOffsetMs, 3600000);
});

test("applyHealth reads a device clock that is behind as a negative offset", () => {
  resetState();
  const agentNow = "2026-09-14T10:00:00Z";

  applyHealth({ now: agentNow, sources: {} }, Date.parse(agentNow) - 120000);

  assert.equal(state.clockOffsetMs, -120000);
});

test("applyHealth keeps no offset when the agent sends no usable time", () => {
  resetState();

  applyHealth({ sources: {} }, Date.now());
  assert.equal(state.clockOffsetMs, 0);

  applyHealth({ now: "not a timestamp", sources: {} }, Date.now());
  assert.equal(state.clockOffsetMs, 0);
});

test("relative age ignores a device clock that is an hour ahead", () => {
  const offset = 3600000;
  // Five seconds old on the agent's clock, which the device sees an hour late.
  const stamp = new Date(Date.now() - offset - 5000).toISOString();

  assert.equal(relativeAge(stamp, offset), "5s");
  assert.equal(relativeAge(stamp), "1h", "without the offset the skew shows up as age");
});

test("relative age ignores a device clock that is behind", () => {
  const offset = -120000;
  const stamp = new Date(Date.now() - offset - 5000).toISOString();

  assert.equal(relativeAge(stamp, offset), "5s");
});
