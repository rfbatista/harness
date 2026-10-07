import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryChannel } from "../../infrastructure/memory-channel.js";
import { makeCheck } from "../../testing/channel-fixtures.js";
import { T0 } from "../../testing/fixtures.js";
import { statusCheck } from "./statusCheck.js";

file("sessions/presentation/statusCheck");

function make(check, { remembered } = {}) {
  const channel = memoryChannel({ checks: check ? [check] : [], now: () => T0 });
  const mounted = mount(() => statusCheck({ channel: channel.gateway })({ key: "k", sessionId: "d1", check, remembered }));
  return { ...mounted, channel };
}

test("an active loop pauses, and says so to the page", async () => {
  const { instance, dispatched } = make(makeCheck({ everyMinutes: 10 }));
  assert.deepEqual([instance.minutes, instance.toggleWord, instance.controllable], ["10", "Pause", true]);
  await instance.toggle();
  const [changed] = dispatched.filter((d) => d.name === "status-check-changed");
  assert.deepEqual([changed.detail.check.state, changed.detail.check.everyMinutes], ["paused", 0]);
});

test("a paused loop resumes at the interval it had before the pause, or the default", async () => {
  const remembered = make(makeCheck({ state: "paused", everyMinutes: 0, nextAt: null }), { remembered: 30 });
  assert.deepEqual([remembered.instance.minutes, remembered.instance.toggleWord], ["30", "Resume"]);
  await remembered.instance.toggle();
  assert.equal(remembered.dispatched.at(-1).detail.check.everyMinutes, 30);

  const fresh = make(makeCheck({ state: "paused", everyMinutes: 0, nextAt: null }));
  assert.equal(fresh.instance.minutes, "10");
});

test("a new interval retunes an active loop at once; a paused one keeps it for Resume", async () => {
  const active = make(makeCheck({ everyMinutes: 10 }));
  active.instance.minutes = "60";
  await active.instance.retune();
  assert.equal(active.dispatched.at(-1).detail.check.everyMinutes, 60);

  const paused = make(makeCheck({ state: "paused", everyMinutes: 0, nextAt: null }));
  paused.instance.minutes = "60";
  await paused.instance.retune();
  assert.equal(paused.dispatched.length, 0, "nothing sent while paused");
  assert.equal(paused.channel.check("d1").state, "paused");
});

test("an ended loop offers no controls", () => {
  const { instance } = make(makeCheck({ state: "ended" }));
  assert.equal(instance.controllable, false);
});

test("a refused change shows the coded error and leaves the loop as it was", async () => {
  const { instance, dispatched } = make(null);
  await instance.set(10);
  assert.equal(instance.error.code, Codes.STATUS_CHECK_NOT_FOUND);
  assert.equal(instance.busy, false);
  assert.equal(dispatched.length, 0);
  instance.dismissError();
  assert.equal(instance.error, null);
});

test("the intervals on offer are the server's, labelled for people", () => {
  const { instance } = make(makeCheck());
  assert.deepEqual(instance.intervals.map((i) => i.label), ["2 min", "5 min", "10 min", "15 min", "30 min", "1 h", "2 h", "4 h"]);
});
