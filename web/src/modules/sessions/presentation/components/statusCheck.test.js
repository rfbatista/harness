import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryChannel } from "../../infrastructure/memory-channel.js";
import { makeCheck } from "../../testing/channel-fixtures.js";
import { T0 } from "../../testing/fixtures.js";
import { statusCheck } from "./statusCheck.js";

file("sessions/presentation/statusCheck");

function make(check, { remembered, next = "next in 15m", last = "no check yet" } = {}) {
  const channel = memoryChannel({ checks: check ? [check] : [], now: () => T0 });
  const mounted = mount(() => statusCheck({ channel: channel.gateway })({ key: "k", sessionId: "d1", check, remembered, next, last }));
  return { ...mounted, channel };
}

test("an active loop pauses, and says so to the page", async () => {
  const { instance, dispatched } = make(makeCheck({ everyMinutes: 10 }));
  assert.deepEqual([instance.minutes, instance.toggleWord, instance.controllable], ["10", "Pause checks", true]);
  await instance.toggle();
  const [changed] = dispatched.filter((d) => d.name === "status-check-changed");
  assert.deepEqual([changed.detail.check.state, changed.detail.check.everyMinutes], ["paused", 0]);
});

test("the picker offers the loop's own interval, in order, when it is not one of the presets", () => {
  const { instance } = make(makeCheck({ everyMinutes: 20 }));
  assert.equal(instance.minutes, "20");
  const values = instance.intervals.map((i) => i.value);
  assert.deepEqual(values, ["2", "5", "10", "15", "20", "30", "60", "120", "240"], "the architect set 20 minutes: the select can show it");
  assert.equal(instance.intervals.find((i) => i.value === "20").label, "20 min");

  const odd = make(makeCheck({ everyMinutes: 90 }));
  assert.equal(odd.instance.intervals.find((i) => i.value === "90").label, "90 min", "not a whole number of hours: minutes");

  const preset = make(makeCheck({ everyMinutes: 10 }));
  assert.equal(preset.instance.intervals.length, 8, "a preset is listed once");
});

test("a paused loop resumes at the interval it had before the pause, or the default", async () => {
  const remembered = make(makeCheck({ state: "paused", everyMinutes: 0, nextAt: null }), { remembered: 30 });
  assert.deepEqual([remembered.instance.minutes, remembered.instance.toggleWord], ["30", "Resume checks"]);
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

test("the checks button and its panel read from the one interval the select shows", () => {
  const { instance } = make(makeCheck({ everyMinutes: 20 }));
  assert.deepEqual([instance.every, instance.buttonDot, instance.buttonNext], ["20m", "live", "next 15m"]);
  assert.equal(instance.buttonLabel, "Status checks: every 20m, next in 15m");
  assert.deepEqual([instance.panelDot, instance.panelWord, instance.toggleWord], ["live", "On", "Pause checks"]);
  instance.minutes = "60";
  assert.equal(instance.every, "1h", "the button follows the select");
});

test("paused, saving and a failed change each read on the button", async () => {
  const paused = make(makeCheck({ state: "paused", everyMinutes: 0, nextAt: null }), { remembered: 30 });
  assert.deepEqual([paused.instance.buttonDot, paused.instance.buttonNext, paused.instance.panelWord, paused.instance.toggleWord], ["idle", "paused", "Paused", "Resume checks"]);

  const failing = make(null);
  await failing.instance.set(10);
  assert.deepEqual([failing.instance.buttonDot, failing.instance.buttonNext], ["failed", "couldn't save"]);

  const saving = make(makeCheck({ everyMinutes: 20 }));
  saving.instance.busy = true;
  assert.equal(saving.instance.buttonNext, "saving…");
});

test("Retry sends the change that failed again", async () => {
  const { instance, channel } = make(makeCheck({ everyMinutes: 10 }));
  channel.failNext?.();
  instance.minutes = "30";
  await instance.retune();
  await instance.retry();
  assert.equal(channel.check("d1").everyMinutes, 30);
});

test("the button opens the panel, anchored to itself", () => {
  const { instance, dispatched } = make(makeCheck({ everyMinutes: 20 }));
  const button = document.createElement("button");
  Object.defineProperty(instance, "$refs", { value: { button } });
  instance.openPanel();
  assert.deepEqual(dispatched.filter((d) => d.name === "dialog-open").map((d) => d.detail), [{ id: "session-checks", anchor: button }]);
});
