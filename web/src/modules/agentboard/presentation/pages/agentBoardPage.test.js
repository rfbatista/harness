import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryBoard } from "../../infrastructure/memory-gateway.js";
import {
  ANNOUNCE_MS,
  EXIT_MS,
  HOLD_MS,
  OFFLINE_AFTER_MS,
  RECONNECTING_AFTER_MS,
  SKELETON_AFTER_MS,
  agentBoardPage,
} from "./agentBoardPage.js";

file("agentboard/presentation/agentBoardPage");

const NOW = new Date("2026-11-06T14:00:00Z");
const ago = (minutes) => new Date(NOW.getTime() - minutes * 60_000).toISOString();

// ---- the wire, as the server sends it ----
const line = (id, agent, status, minutes = 1, extra = {}) => ({
  session_id: id,
  agent_name: agent,
  role: "delegate",
  status,
  status_since: ago(minutes),
  waiting_for: null,
  mode: "",
  model: "",
  ...extra,
});

const NEEDS = new Set(["idle", "waiting_approval"]);

/** A card; needs_you is set the way the server would (the page never derives it). */
const card = (task, project, title, sessions, extra = {}) => {
  const needs = sessions.some((s) => NEEDS.has(s.status) || s.open_interview_count > 0);
  return {
    task_id: task,
    project_id: project,
    task_title: title,
    task_status: "in_progress",
    feature_id: null,
    feature_title: null,
    needs_you: needs,
    needs_you_since: needs ? ago(5) : null,
    last_status_change_at: ago(1),
    sessions,
    ...extra,
  };
};

const seedOf = (projects, extra = {}) => ({
  seq: 10,
  generated_at: NOW.toISOString(),
  project_count: 5,
  quiet_projects: 2,
  projects: projects.map(([id, name, cards]) => ({ project_id: id, project_name: name, cards })),
  ...extra,
});

const alpha = () => card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "running")]);
const alpha2 = () => card("t-a2", "p-alpha", "Audit log", [line("s2", "backend", "running")]);
const beta = () => card("t-b1", "p-beta", "Landing page", [line("s3", "copywriter", "thinking")], { feature_id: "f1", feature_title: "Brand" });
const SEED = () => seedOf([["p-alpha", "alpha", [alpha(), alpha2()]], ["p-beta", "Beta", [beta()]]]);

let seq = 10;
const live = (c, extra = {}) => ({ seq: ++seq, kind: "session_live", project_id: c.project_id, task_id: c.task_id, session: c.sessions[0], card: c, project_name: "", ...extra });
const ended = (c, sessionId, remaining = null, extra = {}) => ({ seq: ++seq, kind: "session_ended", project_id: c.project_id, task_id: c.task_id, session_id: sessionId, ended: "done", card: remaining, ...extra });

async function setup({ seed = SEED(), visible = true, reduced = false, withSeedInPage = true } = {}) {
  seq = seed?.seq ?? 0;
  const memory = memoryBoard({ seed });
  const timers = manualTimers();
  const visited = [];
  const state = { visible, reduced };
  const doc = { title: "Agent board · harness" };
  const el = withSeedInPage ? seededElement(seed) : seededElement(null);
  if (!withSeedInPage) delete el.dataset.seed;
  const mounted = mount(
    agentBoardPage({
      gateway: memory.gateway,
      navigate: (url) => visited.push(url),
      clock: fixedClock(NOW),
      timers,
      isVisible: () => state.visible,
      reducedMotion: () => state.reduced,
      doc,
    }),
    { el },
  );
  mounted.instance.init();
  mounted.tick();
  await flush();
  return { ...mounted, memory, timers, visited, state, doc };
}

const press = (instance, key, target = { tagName: "MAIN" }) => {
  const event = { key, target, defaultPrevented: false, prevented: false, preventDefault() { this.prevented = true; } };
  instance.key(event);
  return event;
};

const names = (instance) => instance.sections.map((s) => [s.name, s.cards.map((c) => c.id)]);
const card1 = (instance, id) => instance.sections.flatMap((s) => s.cards).find((c) => c.id === id);

// ---- the seed ----

test("paints the seed into project sections and cards in the server's order, and links cards and sessions", async () => {
  const { instance } = await setup();
  assert.deepEqual(names(instance), [["alpha", ["t-a1", "t-a2"]], ["Beta", ["t-b1"]]]);
  assert.equal(instance.ready, true);
  const c = card1(instance, "t-b1");
  assert.equal(c.feature, "Brand");
  assert.equal(c.href, "/projects/p-beta/tasks/t-b1");
  assert.equal(c.lines[0].href, "/projects/p-beta/tasks/t-b1?session=s3");
  assert.equal(c.status, "In progress");
  assert.deepEqual(instance.summary, { lead: "3 tasks with 3 live sessions in 2 projects", needs: "", hasNeeds: false, needsCount: 0, cards: 3 });
  assert.equal(instance.quiet, "2 quiet projects have no live session.");
});

test("needs_you is the server's word: the accent follows the flag, not the session statuses", async () => {
  const wrong = card("t-x", "p-alpha", "Flag says no", [line("sx", "a", "waiting_approval")], { needs_you: false });
  const { instance } = await setup({ seed: seedOf([["p-alpha", "alpha", [wrong]]]) });
  assert.equal(card1(instance, "t-x").needsYou, null);
  assert.equal(instance.needsCount, 0);
});

test("a card shows approvals first, then your turn, then the rest in the server's order", async () => {
  const c = card("t1", "p", "Busy", [line("a", "one", "running"), line("b", "two", "idle"), line("c", "three", "waiting_approval", 1, { waiting_for: "Run: git push" }), line("d", "four", "thinking")]);
  const { instance } = await setup({ seed: seedOf([["p", "proj", [c]]]) });
  const view = card1(instance, "t1");
  assert.deepEqual(view.lines.map((l) => [l.id, l.word, l.dot, l.chip]), [["c", "approval", "waiting", true], ["b", "your turn", "waiting", true], ["a", "running", "running", false]]);
  assert.equal(view.lines[0].waitingFor, "Run: git push");
  assert.equal(view.hasMore, true);
  assert.equal(view.moreLabel, "+1 more");
});

test("a session with an open interview reads questions, even while it runs: after approval, before your turn", async () => {
  const c = card("t1", "p", "Busy", [
    line("a", "one", "running"),
    line("b", "two", "idle"),
    line("e", "five", "running", 1, { open_interview_count: 1, open_question_count: 3 }),
    line("c", "three", "waiting_approval", 1, { waiting_for: "Run: git push", open_interview_count: 1, open_question_count: 2 }),
    line("d", "four", "idle", 1, { open_interview_count: 2, open_question_count: 4 }),
  ]);
  const { instance } = await setup({ seed: seedOf([["p", "proj", [c]]]) });
  const view = card1(instance, "t1");
  assert.equal(view.needsYou, true, "the server's flag: an open interview makes the task need the person");
  const lines = [...view.lines];
  assert.deepEqual(lines.map((l) => [l.id, l.word, l.dot, l.tone, l.chip]), [
    ["c", "approval", "waiting", "attention", true],
    ["e", "questions", "waiting", "attention", true],
    ["d", "questions", "waiting", "attention", true],
  ]);
  assert.equal(lines[1].hover.includes("questions"), true);
});

test("a count that changes on a running session changes its line in place, with the wash", async () => {
  const { instance, memory } = await setup();
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "running", 1, { open_interview_count: 1, open_question_count: 2 })])));
  const l = card1(instance, "t-a1").lines[0];
  assert.deepEqual([l.word, l.fresh], ["questions", true]);
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "running", 1)])));
  assert.equal(card1(instance, "t-a1").lines[0].word, "running", "answered: the chip goes");
});

test("nothing live shows the empty state and no project at all never reaches the page", async () => {
  const { instance } = await setup({ seed: seedOf([], { quiet_projects: 5 }) });
  assert.equal(instance.showEmpty, true);
  assert.equal(instance.showBoard, false);
  assert.equal(instance.summary.lead, "No live sessions right now.");
});

test("a malformed seed is read again from the server instead of drawing half a board", async () => {
  const memory = memoryBoard({ seed: SEED() });
  const el = seededElement({ projects: "nope" });
  const { instance, tick } = mount(agentBoardPage({ gateway: memory.gateway, navigate() {}, clock: fixedClock(NOW), timers: manualTimers(), doc: { title: "" } }), { el });
  instance.init();
  tick();
  await flush();
  await flush();
  assert.equal(memory.reads.count, 1);
  assert.equal(instance.loaded, true);
});

// ---- order and live changes ----

test("a session that starts makes its card arrive at the head of the cards that do not need the person", async () => {
  const waits = card("t-w", "p-alpha", "Waits", [line("sw", "arch", "idle")]);
  const { instance, memory } = await setup({ seed: seedOf([["p-alpha", "alpha", [waits, alpha()]]]) });
  const fresh = card("t-new", "p-alpha", "Brand new", [line("sn", "dev", "starting", 0)]);
  memory.publish(live(fresh));
  assert.deepEqual(names(instance), [["alpha", ["t-w", "t-new", "t-a1"]]]);
  assert.equal(card1(instance, "t-new").arriving, true);
  assert.equal(card1(instance, "t-new").fresh, true);
});

test("a card of a project with no section makes the section, in name order", async () => {
  const { instance, memory } = await setup();
  const c = card("t-n", "p-az", "Zeta work", [line("sz", "dev", "running")]);
  memory.publish(live(c, { project_name: "azure" }));
  assert.deepEqual(instance.sections.map((s) => s.name), ["alpha", "azure", "Beta"]);
});

test("a status change updates the line in place with a highlight and moves nothing", async () => {
  const { instance, memory, timers } = await setup();
  const before = names(instance);
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "thinking", 0)])));
  assert.deepEqual(names(instance), before);
  const l = card1(instance, "t-a1").lines[0];
  assert.equal(l.word, "thinking");
  assert.equal(l.fresh, true);
  assert.equal(l.time, "now");
  timers.tick();
  assert.equal(card1(instance, "t-a1").lines[0].fresh, null, "the wash is one-shot");
});

test("a card that starts needing the person moves to the front; one that stops needing them stays put", async () => {
  const { instance, memory } = await setup();
  const asks = card("t-a2", "p-alpha", "Audit log", [line("s2", "backend", "waiting_approval", 0, { waiting_for: "Run: pnpm migrate" })]);
  memory.publish(live(asks));
  assert.deepEqual(names(instance)[0], ["alpha", ["t-a2", "t-a1"]]);
  assert.equal(card1(instance, "t-a2").needsYou, true);
  assert.equal(instance.sections[0].needs, "1 needs you");

  const calm = card("t-a2", "p-alpha", "Audit log", [line("s2", "backend", "running", 0)]);
  memory.publish(live(calm));
  assert.deepEqual(names(instance)[0], ["alpha", ["t-a2", "t-a1"]], "it keeps its place until the next seed");
  assert.equal(card1(instance, "t-a2").needsYou, null);
  assert.equal(instance.sections[0].needs, "");
});

test("task_changed and project_changed rename the card and the section in place; a project rename re-sorts once", async () => {
  const { instance, memory } = await setup();
  const renamed = { ...alpha(), task_title: "Retry policy v2", task_status: "review", feature_title: "Webhooks" };
  memory.publish({ seq: ++seq, kind: "task_changed", project_id: "p-alpha", task_id: "t-a1", card: renamed });
  const c = card1(instance, "t-a1");
  assert.deepEqual([c.title, c.status, c.feature], ["Retry policy v2", "Review", "Webhooks"]);
  memory.publish({ seq: ++seq, kind: "project_changed", project_id: "p-alpha", project_name: "zulu" });
  assert.deepEqual(instance.sections.map((s) => s.name), ["Beta", "zulu"]);
});

test("the summary and the section counts follow the cards; the tab title ends with how many tasks need the person", async () => {
  const { instance, memory, doc } = await setup();
  assert.equal(doc.title, "Agent board · harness");
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "idle", 0)])));
  assert.equal(doc.title, "Agent board · harness · 1 needs you");
  assert.equal(instance.summary.needs, "1 need you");
  assert.equal(instance.sections[0].count, "2 tasks · 2 live");
  memory.publish(live(card("t-b1", "p-beta", "Landing page", [line("s3", "copywriter", "waiting_approval", 0)])));
  assert.equal(doc.title, "Agent board · harness · 2 need you");
});

test("the notification bell's count and the board's coexist in the tab title, whichever writes last", async () => {
  const { memory, doc } = await setup();
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "idle", 0)])));
  doc.title = "(3) " + doc.title; // the bell store leads with its own count
  memory.publish(live(card("t-b1", "p-beta", "Landing page", [line("s3", "copywriter", "waiting_approval", 0)])));
  assert.equal(doc.title, "(3) Agent board · harness · 2 need you");
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "running", 0)])));
  memory.publish(live(card("t-b1", "p-beta", "Landing page", [line("s3", "copywriter", "running", 0)])));
  assert.equal(doc.title, "(3) Agent board · harness", "no mark when nobody needs the person");
});

// ---- a card that leaves ----

test("a card whose last session ended is held, then fades and goes; a held card shows its line finished", async () => {
  const { instance, memory, timers } = await setup();
  memory.publish(ended(alpha(), "s1"));
  const held = card1(instance, "t-a1");
  assert.deepEqual([held.lines[0].word, held.lines[0].finished, held.leaving], ["finished", true, null]);
  assert.equal(instance.summary.cards, 2, "a leaving card is not counted");
  assert.ok(timers.delays().includes(HOLD_MS));
  timers.tick();
  assert.equal(card1(instance, "t-a1").leaving, true, "fading");
  assert.ok(timers.delays().includes(EXIT_MS));
  timers.tick();
  assert.deepEqual(names(instance)[0], ["alpha", ["t-a2"]]);
});

test("a card under the pointer or with focus is not removed until the pointer or focus leaves", async () => {
  const { instance, memory, timers } = await setup();
  memory.publish(ended(alpha(), "s1"));
  instance.hover("t-a1");
  timers.tick(); // the hold is over
  assert.equal(card1(instance, "t-a1").leaving, null, "still there, not fading");
  timers.tick();
  assert.ok(card1(instance, "t-a1"));
  instance.unhover("t-a1");
  assert.equal(card1(instance, "t-a1").leaving, true);
  timers.tick();
  assert.equal(card1(instance, "t-a1"), undefined);

  memory.publish(ended(alpha2(), "s2"));
  instance.focused("t-a2");
  timers.tick();
  timers.tick();
  assert.ok(card1(instance, "t-a2"), "focus holds it");
  instance.blurred("t-a2", { currentTarget: { contains: () => false }, relatedTarget: null });
  timers.tick();
  assert.equal(card1(instance, "t-a2"), undefined);
});

test("focus moving inside a card is not leaving it; a session that starts again keeps the card", async () => {
  const { instance, memory, timers } = await setup();
  memory.publish(ended(alpha(), "s1"));
  instance.focused("t-a1");
  instance.blurred("t-a1", { currentTarget: { contains: () => true }, relatedTarget: {} });
  assert.equal(instance.focusId, "t-a1");
  instance.blurred("t-a1", { currentTarget: { contains: () => false }, relatedTarget: null });
  memory.publish(live(alpha()));
  timers.tick();
  timers.tick();
  assert.equal(card1(instance, "t-a1").lines[0].word, "running", "it came back before the hold ended");
});

test("a session that ends on a card with others live updates it in place", async () => {
  const two = card("t2", "p", "Two", [line("a", "one", "running"), line("b", "two", "running")]);
  const { instance, memory } = await setup({ seed: seedOf([["p", "proj", [two]]]) });
  memory.publish(ended(two, "a", card("t2", "p", "Two", [line("b", "two", "running")])));
  assert.deepEqual(card1(instance, "t2").lines.map((l) => l.id), ["b"]);
});

// ---- seq, gaps and the seed read again ----

test("events at or below the seed's seq are discarded", async () => {
  const { instance, memory } = await setup();
  memory.publish({ ...live(card("t-z", "p-alpha", "Old news", [line("sz", "dev", "running")])), seq: 10 });
  memory.publish({ ...live(card("t-z", "p-alpha", "Old news", [line("sz", "dev", "running")])), seq: 3 });
  assert.equal(card1(instance, "t-z"), undefined);
  assert.equal(memory.reads.count, 0);
});

test("a gap makes exactly one seed read, reconciled by task; the events that came meanwhile are applied after it", async () => {
  const { instance, memory } = await setup();
  const gone = SEED();
  gone.seq = 14;
  gone.projects[0].cards = [alpha2()]; // t-a1 ended while the feed was not heard
  memory.setSeed(gone);
  memory.holdReads();
  memory.publish({ ...live(card("t-q", "p-alpha", "Skipped", [line("sq", "dev", "running")])), seq: 13 }); // 11 and 12 missed
  memory.publish({ ...live(card("t-r", "p-alpha", "During the read", [line("sr", "dev", "running")])), seq: 15 });
  memory.publish({ ...live(card("t-r", "p-alpha", "During the read", [line("sr", "dev", "running")])), seq: 16 });
  assert.equal(memory.reads.count, 1, "one read, not one per event");
  memory.answer();
  await flush();
  assert.equal(memory.reads.count, 1);
  assert.equal(instance.board.seq, 16);
  assert.ok(card1(instance, "t-r"), "the events after the seed are applied");
  assert.equal(card1(instance, "t-q"), undefined, "the one at or below the seed is the seed's business");
  assert.equal(card1(instance, "t-a1").lines[0].word, "finished", "a card the seed lacks leaves like an ended one");
});

test("a seed with a lower seq replaces the state wholesale", async () => {
  const { instance, memory } = await setup();
  const restarted = seedOf([["p-gamma", "gamma", [card("t-g", "p-gamma", "After restart", [line("sg", "dev", "running")])]]], { seq: 2 });
  memory.setSeed(restarted);
  memory.reportStatus(FeedStatus.PAUSED);
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(names(instance), [["gamma", ["t-g"]]]);
  assert.equal(instance.board.seq, 2);
  memory.publish({ ...live(card("t-h", "p-gamma", "Next", [line("sh", "dev", "running")])), seq: 3 });
  assert.ok(card1(instance, "t-h"));
});

test("resynced reads the seed again; the stream bar hears every status", async () => {
  const { memory, dispatched } = await setup();
  assert.deepEqual(dispatched.map((d) => d.detail), [FeedStatus.LIVE]);
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(memory.reads.count, 1);
});

// ---- feed health ----

test("a feed that stays down gets the reconnecting banner after 2 s and the offline one after 10 s, cards untouched; coming back clears it", async () => {
  const { instance, memory, timers } = await setup();
  assert.equal(instance.banner, null);
  memory.reportStatus(FeedStatus.PAUSED);
  assert.deepEqual(timers.delays().filter((d) => d === RECONNECTING_AFTER_MS || d === OFFLINE_AFTER_MS), [RECONNECTING_AFTER_MS, OFFLINE_AFTER_MS]);
  timers.tick();
  assert.equal(instance.banner.tone, "danger", "both timers fired in this test");
  assert.equal(instance.banner.role, "alert");
  assert.ok(instance.banner.text.startsWith("The board stopped updating at "));
  assert.equal(instance.sections.length, 2);
  memory.reportStatus(FeedStatus.LIVE);
  assert.equal(instance.banner, null);
});

test("the reconnecting banner says the cards may be out of date, as of the last update", async () => {
  const { instance } = await setup();
  instance.down = "reconnecting";
  assert.equal(instance.banner.lead, "Reconnecting.");
  assert.ok(instance.banner.text.startsWith("What you see is as of "));
  assert.equal(instance.banner.action, "Retry now");
});

test("retry now reads the seed again", async () => {
  const { instance, memory } = await setup();
  await instance.retry();
  assert.equal(memory.reads.count, 1);
});

// ---- reading ----

test("more than three lines collapse behind +N more, which expands in place and stays expanded across updates", async () => {
  const c = card("t1", "p", "Busy", ["a", "b", "c", "d", "e"].map((id) => line(id, `agent-${id}`, "running")));
  const { instance, memory } = await setup({ seed: seedOf([["p", "proj", [c]]]) });
  assert.equal(card1(instance, "t1").lines.length, 3);
  assert.equal(card1(instance, "t1").moreLabel, "+2 more");
  instance.toggleMore("t1");
  assert.equal(card1(instance, "t1").lines.length, 5);
  assert.equal(card1(instance, "t1").moreLabel, "Show fewer");
  memory.publish(live(card("t1", "p", "Busy", ["a", "b", "c", "d", "e"].map((id) => line(id, `agent-${id}`, id === "a" ? "thinking" : "running", 0)))));
  assert.equal(card1(instance, "t1").lines.length, 5, "an update does not fold it");
  instance.toggleMore("t1");
  assert.equal(card1(instance, "t1").lines.length, 3);
});

test("the filter narrows cards by task, feature, project and agent, hides empty sections, and shows no match", async () => {
  const { instance } = await setup();
  instance.query = "brand";
  assert.deepEqual(names(instance), [["Beta", ["t-b1"]]], "the feature's title (and the task's)");
  instance.query = "ALPHA";
  assert.deepEqual(names(instance), [["alpha", ["t-a1", "t-a2"]]], "the project, any case");
  instance.query = "copywriter";
  assert.deepEqual(names(instance), [["Beta", ["t-b1"]]], "an agent");
  instance.query = "audit";
  assert.deepEqual(names(instance), [["alpha", ["t-a2"]]]);
  instance.query = "nothing like it";
  assert.equal(instance.noMatch, true);
  assert.equal(instance.noMatchText, "No task, project or agent matches “nothing like it”.");
  instance.clearFilter();
  assert.equal(instance.query, "");
  assert.equal(instance.sections.length, 2);
});

test("J and K move between cards across sections, Enter opens the one the cursor is on, / filters, typing in a field is left alone", async () => {
  const { instance, visited } = await setup();
  assert.ok(press(instance, "j").prevented);
  assert.equal(instance.cursorId, "t-a1");
  press(instance, "j");
  press(instance, "j");
  assert.equal(instance.cursorId, "t-b1", "across the section");
  press(instance, "j");
  assert.equal(instance.cursorId, "t-b1", "stops at the end");
  press(instance, "k");
  assert.equal(instance.cursorId, "t-a2");
  press(instance, "Enter");
  assert.deepEqual(visited, ["/projects/p-alpha/tasks/t-a2"]);

  let focused = 0;
  instance.$refs = { filter: { focus: () => focused++ } };
  press(instance, "/");
  assert.equal(focused, 1);

  assert.equal(press(instance, "j", { tagName: "INPUT" }).prevented, false);
  assert.equal(press(instance, "Enter", { tagName: "A" }).prevented, false, "a focused link opens itself");
  assert.equal(visited.length, 1);
});

test("K from nowhere lands on the last card; Esc in the filter clears it and gives the keys back", async () => {
  const { instance } = await setup();
  press(instance, "k");
  assert.equal(instance.cursorId, "t-b1");
  instance.query = "x";
  let blurred = 0;
  instance.$refs = { filter: { blur: () => blurred++ } };
  instance.clearFilter();
  assert.deepEqual([instance.query, blurred], ["", 1]);
});

// ---- announcements ----

test("arrivals and needs-you changes are announced politely, one sentence per burst; status changes are not", async () => {
  const { instance, memory, timers } = await setup();
  memory.publish(live(card("t-a1", "p-alpha", "Retry policy", [line("s1", "backend", "thinking", 0)])));
  timers.tick();
  assert.equal(instance.liveText, "", "an ordinary status change says nothing");

  memory.publish(live(card("t-n1", "p-alpha", "One", [line("n1", "dev", "starting", 0)])));
  memory.publish(live(card("t-n2", "p-alpha", "Two", [line("n2", "dev", "starting", 0)])));
  memory.publish(live(card("t-a2", "p-alpha", "Audit log", [line("s2", "backend", "waiting_approval", 0)])));
  memory.publish(live(card("t-b1", "p-beta", "Landing page", [line("s3", "copywriter", "idle", 0)])));
  assert.equal(instance.liveText, "", "nothing is said until the burst settles");
  assert.ok(timers.delays().includes(ANNOUNCE_MS));
  timers.tick();
  assert.equal(instance.liveText, "alpha: One started. alpha: Two started. 2 more updates.");
});

test("a single needs-you change names the card and why", async () => {
  const { instance, memory, timers } = await setup();
  memory.publish(live(card("t-a2", "p-alpha", "Audit log", [line("s2", "backend", "waiting_approval", 0)])));
  timers.tick();
  assert.equal(instance.liveText, "alpha: Audit log needs you, approval.");
});

test("a hidden tab applies changes without highlights or announcements", async () => {
  const { instance, memory, timers, state } = await setup({ visible: false });
  memory.publish(live(card("t-n1", "p-alpha", "One", [line("n1", "dev", "starting", 0)])));
  assert.ok(card1(instance, "t-n1"));
  assert.equal(card1(instance, "t-n1").fresh, null);
  assert.equal(card1(instance, "t-n1").arriving, null);
  timers.tick();
  assert.equal(instance.liveText, "");
  state.visible = true;
});

// ---- a page swapped in place ----

test("a page swapped in without a seed reads it; the skeleton shows only when the read is slower than 400 ms", async () => {
  const fast = await setup({ withSeedInPage: false });
  assert.equal(fast.instance.loaded, true, "a fast read shows no skeleton");
  assert.equal(fast.instance.skeletonShown, false);

  const held = memoryBoard({ seed: SEED() });
  held.holdReads();
  const t = manualTimers();
  const el = seededElement(null);
  delete el.dataset.seed;
  const page = mount(agentBoardPage({ gateway: held.gateway, navigate() {}, clock: fixedClock(NOW), timers: t, doc: { title: "" } }), { el });
  page.instance.init();
  await flush();
  assert.ok(t.delays().includes(SKELETON_AFTER_MS));
  assert.equal(page.instance.skeletonShown, false, "nothing before 400 ms");
  t.tick();
  assert.equal(page.instance.skeletonShown, true);
  assert.equal(page.instance.ready, false);
  held.answer();
  await flush();
  t.tick();
  await flush();
  assert.equal(page.instance.skeletonShown, false);
  assert.equal(page.instance.ready, true);
  assert.equal(page.instance.skeletonIn, "skeleton-in");
  assert.equal(page.instance.sections.length, 2);
});

test("a seed that cannot be read shows the coded error and no half-drawn cards, and trying again reads it", async () => {
  const memory = memoryBoard({ seed: SEED() });
  memory.failNext(new Error("down"));
  const el = seededElement(null);
  delete el.dataset.seed;
  const { instance } = mount(agentBoardPage({ gateway: memory.gateway, navigate() {}, clock: fixedClock(NOW), timers: manualTimers(), doc: { title: "" } }), { el });
  instance.init();
  await flush();
  assert.equal(instance.failed, true);
  assert.equal(instance.banner.code, "BOARD_SEED_FAILED");
  assert.equal(instance.showBoard, false);
  await instance.retry();
  assert.equal(instance.failed, false);
  assert.equal(instance.showBoard, true);
});

test("leaving the page stops the follow and every timer", async () => {
  const { instance, memory, timers } = await setup();
  assert.equal(memory.followers(), 1);
  memory.publish(ended(alpha(), "s1"));
  instance.destroy();
  assert.equal(memory.followers(), 0);
  assert.deepEqual(timers.delays(), []);
});
