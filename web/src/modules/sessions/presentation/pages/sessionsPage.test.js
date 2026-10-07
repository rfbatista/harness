import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { makeArtifact } from "../../testing/artifact-fixtures.js";
import { makeSession, T0, toDTO } from "../../testing/fixtures.js";
import { sessionsPage } from "./sessionsPage.js";

file("sessions/presentation/sessionsPage");

function setup(sessions = [makeSession({ id: "run" }), makeSession({ id: "turn", status: "idle" })]) {
  const memory = memoryGateway({ projects: ["p1"], sessions, now: () => T0 });
  const clock = fixedClock(T0);
  const el = seededElement({
    project_id: "p1",
    ticket_id: "t1",
    sessions: sessions.map(toDTO),
    agent_names: { backend: "Backend dev" },
    repository_names: { r1: "harness" },
  });
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

test("a session that turns terminal on the feed is announced to its Design panel, so it stops following", () => {
  const { instance, memory, dispatched } = setup();
  memory.update("turn", { status: "done" });
  assert.deepEqual(dispatched.filter((d) => d.name === "session-ended"), [{ name: "session-ended", detail: { id: "turn" } }]);
  memory.update("run", { status: "idle" });
  assert.equal(dispatched.filter((d) => d.name === "session-ended").length, 1, "only terminal changes are announced");
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

test("the App tab swaps the agent's terminal for the session's App panel", () => {
  const { instance, tick } = setup([
    makeSession({ id: "live", status: "idle" }),
    makeSession({ id: "loose", status: "idle", repositoryId: "", updatedAt: new Date(T0.getTime() - 60_000) }),
  ]);
  tick();
  assert.ok(instance.agentTabSelected);
  assert.deepEqual(instance.appPanels, []);
  instance.showApp();
  assert.deepEqual(instance.terminalIds, [], "the agent's terminal detaches while the App tab is open");
  assert.deepEqual(instance.appPanels, [
    {
      key: "live",
      sessionId: "live",
      repositoryId: "r1",
      repositoryName: "harness",
      branch: "agent/port-tickets-1a2b3c4d",
      worktree: "/w/harness/.worktrees/port-tickets",
      projectId: "p1",
    },
  ]);
  instance.select("loose");
  assert.deepEqual(instance.appPanels, [], "a session without a repository has no worktree to run from");
  assert.ok(instance.appUnavailable);
  instance.showAgent();
  instance.select("live");
  assert.deepEqual(instance.terminalIds, ["live"]);
  instance.destroy();
});

test("the Design tab mounts the panel for the selected session, alive or ended, and the terminal detaches", () => {
  const { instance, tick } = setup([
    makeSession({ id: "live", status: "idle" }),
    makeSession({ id: "over", status: "done", updatedAt: new Date(T0.getTime() - 60_000) }),
  ]);
  tick();
  assert.deepEqual(instance.designPanels, [{ key: "live:live", sessionId: "live", live: true }], "mounted behind the Agent tab too, so publishes are counted");
  instance.showDesign();
  assert.ok(instance.designTabSelected && instance.showingDesign);
  assert.deepEqual(instance.terminalIds, []);
  assert.deepEqual(instance.appPanels, []);
  instance.select("over");
  assert.deepEqual(instance.designPanels, [{ key: "over:ended", sessionId: "over", live: false }]);
  instance.startCreating();
  assert.deepEqual(instance.designPanels, [], "the form replaces the detail");
  instance.destroy();
});

test("publishes behind another tab count on the Design tab until it is opened; each one is announced", () => {
  const { instance, tick } = setup();
  tick();
  const hero = makeArtifact({ id: "a1", title: "Hero", revision: 1 });
  instance.artifactPublished({ detail: { artifact: hero, isNew: true } });
  assert.equal(instance.designBadge, "1");
  assert.equal(instance.announcement, "New artifact: Hero");
  instance.artifactPublished({ detail: { artifact: makeArtifact({ id: "a1", title: "Hero", revision: 2 }), isNew: false } });
  assert.equal(instance.designBadge, "2");
  assert.equal(instance.announcement, "Artifact updated: Hero, revision 2");
  instance.showDesign();
  assert.equal(instance.designBadge, "", "opening the tab clears the count");
  instance.artifactPublished({ detail: { artifact: makeArtifact({ id: "a2", title: "Card" }), isNew: true } });
  assert.equal(instance.designBadge, "", "nothing to count while the tab is in front");
  instance.showAgent();
  instance.artifactPublished({ detail: { artifact: makeArtifact({ id: "a3", title: "Late" }), isNew: true } });
  assert.equal(instance.designBadge, "1");
  instance.select("run");
  assert.equal(instance.designBadge, "", "another session, another count");
  instance.destroy();
});

test("a session started elsewhere arrives live: highlighted for a moment, announced, naming who started it", async () => {
  const sessions = [makeSession({ id: "lead", agentId: "backend", status: "running" })];
  const memory = memoryGateway({ projects: ["p1"], sessions, now: () => T0 });
  const timers = [];
  const el = seededElement({ project_id: "p1", ticket_id: "t1", sessions: sessions.map(toDTO), agent_names: { backend: "Backend dev" } });
  const { instance } = mount(sessionsPage({ gateway: memory.gateway, clock: fixedClock(T0), setTimeout: (fn, ms) => timers.push({ fn, ms }) }), { el });
  instance.init();
  await flush();

  memory.upsert(makeSession({ id: "peer", task: "Write the feed tests", status: "running", parentSessionId: "lead" }));
  const row = instance.groups.flatMap((g) => g.rows).find((r) => r.id === "peer");
  assert.ok(row, "the new session is in the list");
  assert.ok(row.fresh);
  assert.ok(row.meta.includes("started by Backend dev"), row.meta);
  assert.equal(instance.announcement, "Backend dev started a session: Write the feed tests");

  memory.update("peer", { status: "idle" });
  assert.equal(timers.length, 1, "a change to a session already listed is not an arrival");
  timers[0].fn();
  assert.equal(instance.groups.flatMap((g) => g.rows).find((r) => r.id === "peer").fresh, false);

  memory.upsert(makeSession({ id: "other-task", ticketId: "t2", status: "running" }));
  assert.equal(timers.length, 1, "another task's session is not announced here");
  instance.destroy();
});

test("a session with a worktree links its git history", () => {
  const { instance, tick } = setup([
    makeSession({ id: "on-repo", status: "idle" }),
    makeSession({ id: "loose", status: "idle", workspaceId: "", updatedAt: new Date(T0.getTime() - 60_000) }),
  ]);
  tick();
  instance.select("on-repo");
  assert.equal(instance.historyHref, "/projects/p1/tasks/t1/sessions/on-repo/history");
  instance.select("loose");
  assert.equal(instance.historyHref, "", "no worktree, no history");
  instance.startCreating();
  assert.equal(instance.historyHref, "", "nothing while creating");
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

const endedSession = (overrides = {}) =>
  makeSession({ id: "over", status: "stopped", resumable: true, resumeBlocked: "", updatedAt: new Date(T0.getTime() - 60_000), ...overrides });

test("resume brings the selected ended session back: its terminal mounts on the Agent tab", async () => {
  const { instance, tick } = setup([makeSession({ id: "live", status: "idle" }), endedSession()]);
  tick();
  instance.select("over");
  instance.showDesign();
  assert.equal(instance.selected.resumable, true);
  assert.deepEqual(instance.terminalIds, []);

  const resuming = instance.resume();
  assert.equal(instance.resumingSelected, true, "busy while the call runs");
  await resuming;

  assert.equal(instance.resumingId, null);
  assert.equal(instance.selectedId, "over");
  assert.equal(instance.selected.word, "running");
  assert.equal(instance.selected.resumable, false);
  assert.equal(instance.detailTab, "agent");
  assert.deepEqual(instance.terminalIds, ["over"], "the live terminal attaches");
  assert.ok(instance.announcement.includes("resumed"), instance.announcement);
  instance.destroy();
});

test("resume from a row selects that session and resumes it", async () => {
  const { instance, tick } = setup([makeSession({ id: "live", status: "idle" }), endedSession()]);
  tick();
  assert.equal(instance.selectedId, "live");
  const row = instance.groups.flatMap((g) => g.rows).find((r) => r.id === "over");
  assert.equal(row.resumable, true);
  await instance.resume("over");
  assert.equal(instance.selectedId, "over");
  assert.deepEqual(instance.terminalIds, ["over"]);
  instance.destroy();
});

test("a refused resume shows the coded error, clears the busy state, and refreshes the list", async () => {
  const { instance, memory, tick } = setup([endedSession()]);
  tick();
  instance.select("over");
  memory.update("over", { resumable: false, resumeBlocked: "WORKSPACE_MISSING" });
  instance.sessions = instance.sessions.map((s) => (s.id === "over" ? { ...s, resumable: true, resumeBlocked: "" } : s)); // the page has not heard yet
  await instance.resume();
  assert.equal(instance.error.code, Codes.WORKSPACE_MISSING);
  assert.equal(instance.resumingId, null);

  instance.dismissError();
  memory.update("over", { status: "running", resumable: false, resumeBlocked: "SESSION_ALREADY_RUNNING" });
  instance.sessions = instance.sessions.map((s) => (s.id === "over" ? { ...s, status: "stopped", resumable: true } : s));
  await instance.resume();
  assert.equal(instance.error.code, Codes.SESSION_ALREADY_RUNNING);
  await flush();
  assert.equal(instance.sessions.find((s) => s.id === "over").status, "running", "the list catches up");
  instance.destroy();
});

test("a second Resume while one is in flight makes no second call", async () => {
  const { instance, memory, tick } = setup([endedSession()]);
  tick();
  instance.select("over");
  let calls = 0;
  const resume = memory.gateway.resume;
  memory.gateway.resume = (...args) => {
    calls += 1;
    return resume(...args);
  };
  await Promise.all([instance.resume(), instance.resume()]);
  assert.equal(calls, 1);
  assert.equal(instance.error, null);
  instance.destroy();
});

test("a session resumed elsewhere comes back live without a reload, and is announced", () => {
  const { instance, memory, tick } = setup([endedSession()]);
  tick();
  instance.select("over");
  assert.deepEqual(instance.designPanels.map((p) => p.key), ["over:ended"]);
  memory.update("over", { status: "running", runsOn: "server", resumable: false, resumeBlocked: "SESSION_ALREADY_RUNNING" });
  assert.deepEqual(instance.terminalIds, ["over"]);
  assert.ok(instance.announcement.includes("resumed"), instance.announcement);
  assert.deepEqual(instance.designPanels.map((p) => p.key), ["over:live"], "the Design panel remounts and follows again");
  instance.destroy();
});

test("a session resumed in the TUI points there", () => {
  const { instance, memory, tick } = setup([endedSession()]);
  tick();
  instance.select("over");
  memory.update("over", { status: "running", runsOn: "tui", runnerHost: "laptop", resumable: false, resumeBlocked: "SESSION_ALREADY_RUNNING" });
  assert.deepEqual(instance.terminalIds, []);
  assert.ok(instance.terminalNote.includes("laptop"));
  assert.equal(instance.selected.resumable, false);
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
