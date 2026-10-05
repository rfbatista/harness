import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { makeSession, T0, toDTO } from "../../testing/fixtures.js";
import { sessionsPage } from "./sessionsPage.js";

file("sessions/presentation/sessionsPage");

function setup(sessions = [makeSession({ id: "run" }), makeSession({ id: "turn", status: "idle" })]) {
  const memory = memoryGateway({ projects: ["p1"], sessions, now: () => T0 });
  const clock = fixedClock(T0);
  const el = seededElement({ project_id: "p1", sessions: sessions.map(toDTO) });
  const mounted = mount(sessionsPage({ gateway: memory.gateway, clock }), { el });
  mounted.instance.init();
  return { ...mounted, memory, clock, el };
}

test("starts from the seed and selects what needs the developer first", () => {
  const { instance } = setup();
  assert.equal(instance.projectId, "p1");
  assert.equal(instance.selectedId, "turn");
  assert.equal(instance.summary, "2 sessions · 1 waiting");
  assert.deepEqual(instance.groups.map((g) => g.label), ["Needs you", "Running"]);
  instance.destroy();
});

test("removes the server-rendered rows once Alpine has rendered", () => {
  const { instance, el, tick } = setup();
  const ssr = el.ownerDocument.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  assert.equal(instance.ready, false);
  tick();
  assert.equal(el.querySelector("[data-ssr]"), null);
  assert.equal(instance.ready, true);
  instance.destroy();
});

test("follows the feed: changes regroup the list, a deleted selection clears", async () => {
  const { instance, memory } = setup();
  memory.update("run", { status: "idle" });
  assert.deepEqual(instance.groups.map((g) => `${g.label}:${g.count}`), ["Needs you:2"]);
  memory.remove("turn");
  assert.equal(instance.selectedId, null);
  assert.equal(instance.hasSelection, false);
  instance.destroy();
});

test("reports feed status to the stream bar and reloads after a resync", async () => {
  const { instance, memory, dispatched } = setup();
  await flush();
  assert.deepEqual(dispatched.at(-1), { name: "feed-status", detail: FeedStatus.LIVE });

  memory.upsert(makeSession({ id: "missed", status: "running" }));
  instance.sessions = instance.sessions.filter((s) => s.id !== "missed"); // pretend we missed it
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.ok(instance.sessions.some((s) => s.id === "missed"), "reload brings back what was missed");
  instance.destroy();
});

test("J/K walk the rows in on-screen order", () => {
  const { instance } = setup();
  instance.next();
  assert.equal(instance.selectedId, "run");
  instance.next();
  assert.equal(instance.selectedId, "run", "stops at the end");
  instance.previous();
  assert.equal(instance.selectedId, "turn");
  instance.destroy();
});

test("stops the selected session; a failure shows the coded error", async () => {
  const { instance, memory, tick } = setup();
  tick();
  instance.select("run");
  await instance.stopSelected();
  assert.equal(instance.selected.word, "stopped");
  assert.equal(instance.cannotStop, true, "a stopped session cannot be stopped again");

  instance.select("turn");
  memory.remove("turn");
  instance.selectedId = "turn"; // stale selection: the server no longer has it
  instance.sessions = [...instance.sessions, makeSession({ id: "turn", status: "idle" })];
  await instance.stopSelected();
  assert.equal(instance.error.code, Codes.SESSION_NOT_FOUND);
  instance.dismissError();
  assert.equal(instance.error, null);
  instance.destroy();
});

test("a malformed seed shows an error instead of a broken page", () => {
  const memory = memoryGateway();
  const el = seededElement({ sessions: [] });
  const { instance } = mount(sessionsPage({ gateway: memory.gateway, clock: fixedClock(T0) }), { el });
  instance.init();
  assert.equal(instance.error.code, Codes.BAD_RESPONSE);
  instance.destroy();
});
