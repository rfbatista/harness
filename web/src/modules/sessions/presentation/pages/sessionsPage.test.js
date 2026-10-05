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
  const el = seededElement({ project_id: "p1", ticket_id: "t1", sessions: sessions.map(toDTO), agent_names: { backend: "Backend dev" } });
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

test("follows only its task: other tasks' sessions on the project feed stay out", async () => {
  const { instance, memory } = setup();
  memory.upsert(makeSession({ id: "elsewhere", ticketId: "t2", status: "idle" }));
  assert.equal(instance.sessions.some((s) => s.id === "elsewhere"), false);

  memory.upsert(makeSession({ id: "third", ticketId: "t1", status: "running" }));
  assert.equal(instance.summary, "3 sessions · 1 waiting", "a task runs several sessions at once");

  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(instance.sessions.map((s) => s.id).sort(), ["run", "third", "turn"], "a reload lists only this task");
  instance.destroy();
});

test("a created session joins the task's list, selected, and closes the form", () => {
  const { instance, tick } = setup();
  tick();
  instance.startCreating();
  assert.equal(instance.creating, true);
  assert.equal(instance.showingSession, false, "the form takes the detail pane");

  const session = makeSession({ id: "fresh", ticketId: "t1", status: "starting" });
  instance.sessionCreated({ detail: { session } });
  assert.equal(instance.creating, false);
  assert.equal(instance.selectedId, "fresh");
  assert.equal(instance.sessions.filter((s) => s.id === "fresh").length, 1);
  assert.equal(instance.summary, "3 sessions · 1 waiting");
  instance.destroy();
});

test("N opens the form, but not while typing in a field", () => {
  const { instance, tick } = setup();
  tick();
  const input = document.createElement("input");
  instance.startCreatingFromKey({ target: input });
  assert.equal(instance.creating, false, "typing an n in a field is just typing");
  instance.startCreatingFromKey({ target: document.createElement("div") });
  assert.equal(instance.creating, true);
  instance.destroy();
});

test("delete asks first, then removes the session and clears the selection", async () => {
  const { instance, memory } = setup();
  instance.select("run");
  await instance.deleteSelected(); // not confirmed through the UI, but the action itself is safe to call
  assert.equal(instance.sessions.some((s) => s.id === "run"), false);
  assert.equal(instance.selectedId, null);
  await flush();
  assert.equal((await memory.gateway.list({ projectId: "p1" })).some((s) => s.id === "run"), false);

  instance.select("turn");
  instance.askDelete();
  assert.equal(instance.confirmingDelete, true);
  assert.ok(instance.deleteConsequence.includes("stops now"), "deleting a live session says it stops it");
  instance.cancelDelete();
  assert.equal(instance.confirmingDelete, false);
  assert.ok(instance.sessions.some((s) => s.id === "turn"), "cancel keeps it");
  instance.destroy();
});

test("a failed delete keeps the session and shows the coded error", async () => {
  const { instance, memory } = setup();
  memory.remove("run"); // gone on the server…
  instance.sessions = [...instance.sessions, makeSession({ id: "run" })]; // …while this page still shows it
  instance.select("run");
  await instance.deleteSelected();
  assert.equal(instance.error.code, Codes.SESSION_NOT_FOUND);
  assert.equal(instance.deleting, false);
  instance.destroy();
});

test("the selected live session's terminal is mounted; switching sessions remounts", () => {
  const { instance, tick } = setup([
    makeSession({ id: "live", status: "idle" }),
    makeSession({ id: "over", status: "done", updatedAt: new Date(T0.getTime() - 60_000) }),
  ]);
  tick();
  assert.deepEqual(instance.terminalIds, ["live"]);
  assert.equal(instance.terminalNote, "");
  instance.select("over");
  assert.deepEqual(instance.terminalIds, [], "an ended session has no terminal to attach");
  assert.ok(instance.terminalNote.includes("ended"));
  instance.select("live");
  instance.startCreating();
  assert.equal(instance.creating, true);
  assert.deepEqual(instance.terminalIds, [], "the form replaces the terminal while creating");
  instance.destroy();
});

test("rows show the agent's name", () => {
  const { instance } = setup();
  const row = instance.groups.flatMap((g) => g.rows).find((r) => r.id === "run");
  assert.ok(row.meta.startsWith("Backend dev · "), row.meta);
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
  const el = seededElement({ ticket_id: "t1", sessions: [] });
  const { instance } = mount(sessionsPage({ gateway: memory.gateway, clock: fixedClock(T0) }), { el });
  instance.init();
  assert.equal(instance.error.code, Codes.BAD_RESPONSE);
  instance.destroy();
});
