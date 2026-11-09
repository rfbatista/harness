import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList } from "../testing/interview-fixtures.js";
import { interviewStore, SIGNAL_MS } from "./interviewStore.js";

file("sessions/presentation/interviewStore");

/** A gateway the test drives: events and statuses go to whoever follows, `lists` is what a read answers. */
function fakeGateway(lists = {}) {
  const gw = {
    reads: [],
    lists,
    followers: [],
    stopped: 0,
    async list(id) {
      gw.reads.push(id);
      return gw.lists[id];
    },
    follow(id, onEvent, onStatus) {
      const f = { id, onEvent, onStatus };
      gw.followers.push(f);
      return () => gw.stopped++;
    },
  };
  return gw;
}

function memoryPrefs() {
  const hidden = new Set();
  return { interviewHidden: (id) => hidden.has(id), setInterviewHidden: (id, on) => (on ? hidden.add(id) : hidden.delete(id)) };
}

function fakeTimers() {
  const pending = [];
  return {
    setTimeout: (fn, ms) => pending.push({ fn, ms, done: false }),
    clearTimeout: (n) => {
      if (pending[n - 1]) pending[n - 1].done = true;
    },
    run: (upTo) => pending.filter((p) => !p.done && p.ms <= upTo).forEach((p) => ((p.done = true), p.fn())),
  };
}

/** Drafts held in memory: what was written, what was dropped. */
function memoryDrafts(held = {}) {
  const drafts = {
    held,
    dropped: [],
    read: (id, n) => (held[id]?.length === n ? held[id] : null),
    write: (id, answers) => (held[id] = JSON.parse(JSON.stringify(answers))),
    drop: (id) => {
      drafts.dropped.push(id);
      delete held[id];
    },
    sweep() {},
  };
  return drafts;
}

function setup({ lists = {}, gateway = fakeGateway(), drafts = memoryDrafts() } = {}) {
  const prefs = memoryPrefs();
  const timers = fakeTimers();
  const store = interviewStore({ gateway, prefs, timers, drafts });
  store.seed(lists);
  const said = [];
  return { store, gateway, prefs, timers, drafts, said, announce: (t) => said.push(t) };
}

const asked = (seq, interview) => ({ seq, kind: "interview_asked", interview });

test("docked, an open interview shows the panel with no choice from the person; no interview, no panel", () => {
  const { store } = setup({ lists: { s1: makeList(2, makeInterview()), s2: makeList(0) } });
  assert.equal(store.shown("s1"), true);
  assert.equal(store.shown("s2"), false);
  assert.equal(store.toggle("s2"), null, "the toggle exists while the session has an open interview");
  assert.deepEqual(store.toggle("s1"), { count: "2", label: "Questions, 2 open", pressed: true, signal: false });
});

test("the toggle hides the docked panel and keeps the choice in this session only; the count stays", () => {
  const { store, prefs } = setup({ lists: { s1: makeList(2, makeInterview()), s2: makeList(1, makeInterview({ id: "b", session_id: "s2" })) } });
  store.flip("s1");
  assert.equal(store.shown("s1"), false);
  assert.equal(store.toggle("s1").pressed, false);
  assert.equal(store.toggle("s1").count, "2");
  assert.equal(prefs.interviewHidden("s1"), true);
  assert.equal(store.shown("s2"), true, "another session is not hidden");
  store.flip("s1");
  assert.equal(store.shown("s1"), true);
  assert.equal(prefs.interviewHidden("s1"), false);
});

test("an interview that arrives stays hidden once the person hid the panel, and the toggle rings once", () => {
  const { store, timers, said, announce } = setup({ lists: { s1: makeList(2, makeInterview()) } });
  store.flip("s1");
  store.receive("s1", asked(3, makeInterview({ id: "b" })), announce);
  assert.equal(store.shown("s1"), false);
  assert.equal(store.toggle("s1").count, "4");
  timers.run(0);
  assert.equal(store.toggle("s1").signal, true);
  timers.run(SIGNAL_MS);
  assert.equal(store.toggle("s1").signal, false);
  assert.deepEqual(said, ["architect asked 2 more questions"]);
});

test("narrow, the sheet never opens by itself: a peek strip shows until the person opens it", () => {
  const { store, announce, said } = setup({ lists: { s1: makeList(0) } });
  store.setDocked(false);
  store.receive("s1", asked(1, makeInterview()), announce);
  assert.equal(store.shown("s1"), false);
  assert.equal(store.peeking("s1"), true);
  assert.deepEqual(said, ["architect asks 2 questions"]);
  store.flip("s1");
  assert.equal(store.shown("s1"), true);
  assert.equal(store.peeking("s1"), false);
  store.dismissPanel("s1");
  assert.equal(store.shown("s1"), false);
  store.reveal("s1");
  assert.equal(store.shown("s1"), true);
});

test("the sheet's choice is not the dock's: closing the sheet hides nothing for later, and docking closes it", () => {
  const { store, prefs } = setup({ lists: { s1: makeList(1, makeInterview()) } });
  store.setDocked(false);
  store.reveal("s1");
  store.dismissPanel("s1");
  assert.equal(prefs.interviewHidden("s1"), false);
  store.reveal("s1");
  store.setDocked(true);
  assert.equal(store.sheet, false);
  assert.equal(store.shown("s1"), true);
});

test("events at or before the seed's seq are discarded; the next one is applied", () => {
  const { store } = setup({ lists: { s1: makeList(5, makeInterview({ id: "a" })) } });
  store.receive("s1", asked(5, makeInterview({ id: "dup" })));
  assert.deepEqual(store.list("s1").interviews.map((i) => i.id), ["a"]);
  store.receive("s1", asked(6, makeInterview({ id: "b" })));
  assert.deepEqual(store.list("s1").interviews.map((i) => i.id), ["a", "b"]);
});

test("a gap reads the list again and keeps the newer of it and what is held", async () => {
  const gateway = fakeGateway({ s1: makeList(9, makeInterview({ id: "a" }), makeInterview({ id: "z" })) });
  const { store, announce, said } = setup({ lists: { s1: makeList(5, makeInterview({ id: "a" })) }, gateway });
  store.receive("s1", asked(8, makeInterview({ id: "late" })), announce);
  await flush();
  assert.deepEqual(gateway.reads, ["s1"]);
  assert.deepEqual(store.list("s1").interviews.map((i) => i.id), ["a", "z"]);
  assert.deepEqual(said, ["architect asked 2 more questions"], "z arrived in the read");
  gateway.lists.s1 = makeList(4);
  await store.reload("s1");
  assert.equal(store.list("s1").seq, 9, "an older read is not taken");
});

test("following reads nothing when the seed holds the session, and a resync reads it again", async () => {
  const gateway = fakeGateway({ s1: makeList(7, makeInterview()) });
  const { store, announce } = setup({ lists: { s1: makeList(7, makeInterview()) }, gateway });
  const stop = store.follow("s1", announce);
  await flush();
  assert.deepEqual(gateway.reads, []);
  gateway.followers[0].onStatus("live");
  assert.deepEqual(gateway.reads, []);
  gateway.followers[0].onStatus("resynced");
  assert.deepEqual(gateway.reads, ["s1"]);
  stop();
  assert.equal(gateway.stopped, 1);
  store.follow("s1", announce);
  assert.deepEqual(gateway.reads, ["s1", "s1"], "a session followed again missed what happened in between");
});

test("a session the seed left out is read when its panel follows it", async () => {
  const gateway = fakeGateway({ s3: makeList(2, makeInterview({ id: "n", session_id: "s3" })) });
  const { store, announce } = setup({ gateway });
  store.follow("s3", announce);
  await flush();
  assert.equal(store.shown("s3"), true);
});

test("without a gateway the store only holds what it was seeded with", () => {
  const store = interviewStore();
  store.seed({ s1: makeList(1, makeInterview()) });
  store.follow("s1", () => {})();
  assert.equal(store.shown("s1"), true);
});

const progressOf = (store, id = "iv-1") => store.progress(id);

test("choosing an option, accepting the recommendation, writing own text and a note move the counts", () => {
  const { store } = setup({ lists: { s1: makeList(1, makeInterview()) } });
  assert.deepEqual(progressOf(store), { answered: 0, total: 2, skipped: 2 });
  store.choose("iv-1", 0, "0");
  assert.deepEqual(progressOf(store), { answered: 1, total: 2, skipped: 1 });
  assert.equal(store.isChecked("iv-1", 0, "0"), true);
  assert.equal(store.isChecked("iv-1", 0, "1"), false);
  store.choose("iv-1", 0, "own");
  assert.deepEqual(progressOf(store), { answered: 0, total: 2, skipped: 2 }, "own with no text is not an answer");
  assert.equal(store.ownOpen("iv-1", 0), true);
  store.type("iv-1", 0, "a third way");
  assert.equal(progressOf(store).answered, 1);
  store.note("iv-1", 1, "only a note");
  assert.equal(progressOf(store).answered, 1, "a note is never an answer");
  store.type("iv-1", 1, "in the UI");
  assert.deepEqual(progressOf(store), { answered: 2, total: 2, skipped: 0 });
});

test("a multiple choice ticks and unticks, and a single choice keeps one", () => {
  const multi = makeInterview({ id: "m", questions: [{ title: "t", body: "", kind: "multiple", options: [{ label: "A", description: "" }, { label: "B", description: "" }], recommended: null }] });
  const { store } = setup({ lists: { s1: makeList(1, makeInterview(), multi) } });
  store.choose("m", 0, "0", true);
  store.choose("m", 0, "1", true);
  assert.deepEqual(store.answersFor("m")[0].chosen, [0, 1]);
  store.choose("m", 0, "0", false);
  assert.deepEqual(store.answersFor("m")[0].chosen, [1]);
  store.choose("iv-1", 0, "0");
  store.choose("iv-1", 0, "1");
  assert.deepEqual(store.answersFor("iv-1")[0].chosen, [1]);
});

test("accept all recommended fills only the blank questions that have a recommendation and sends nothing", () => {
  const { store, gateway } = setup({ lists: { s1: makeList(1, makeInterview()) } });
  store.choose("iv-1", 0, "0");
  store.acceptAll("iv-1");
  assert.deepEqual(store.answersFor("iv-1")[0].chosen, [0], "the answer the person gave stays");
  assert.equal(progressOf(store).answered, 1, "the text question has no recommendation");
  const blank = setup({ lists: { s1: makeList(1, makeInterview()) } });
  blank.store.acceptAll("iv-1");
  assert.deepEqual(blank.store.answersFor("iv-1")[0].chosen, [1]);
  assert.deepEqual(gateway.reads, []);
});

test("the answers are the contract's: chosen, text, note, skipped", () => {
  const { store } = setup({ lists: { s1: makeList(1, makeInterview()) } });
  store.choose("iv-1", 0, "1");
  store.note("iv-1", 0, "sure");
  assert.deepEqual(store.submission("iv-1"), [
    { chosen: [1], text: "", note: "sure", skipped: false },
    { chosen: [], text: "", note: "", skipped: true },
  ]);
  assert.equal(store.submission("nope"), null);
});

test("every change is kept as a draft, and a draft is restored when the list is seeded again (a reload)", () => {
  const drafts = memoryDrafts();
  const first = setup({ lists: { s1: makeList(1, makeInterview()) }, drafts });
  first.store.choose("iv-1", 0, "own");
  first.store.type("iv-1", 0, "mine");
  assert.equal(drafts.held["iv-1"][0].text, "mine");
  const reloaded = setup({ lists: { s1: makeList(1, makeInterview()) }, drafts });
  assert.equal(reloaded.store.answersFor("iv-1")[0].text, "mine");
  assert.equal(reloaded.store.ownOpen("iv-1", 0), true);
  assert.equal(progressOf(reloaded.store).answered, 1);
});

test("answers survive the interview being replaced by a changed copy, and drafts of interviews no longer open are dropped", () => {
  const { store, drafts } = setup({ lists: { s1: makeList(1, makeInterview(), makeInterview({ id: "b" })) } });
  store.choose("iv-1", 0, "0");
  store.choose("b", 0, "0");
  store.receive("s1", { seq: 2, kind: "interview_changed", interview: makeInterview({ id: "b", state: "sent", ended_at: "2026-11-08T14:40:00Z", delivery: "queued" }) });
  assert.deepEqual(store.answersFor("iv-1")[0].chosen, [0]);
  assert.deepEqual(drafts.dropped, ["b"]);
  assert.equal(drafts.held.b, undefined);
});

test("drop forgets one interview's answers and its draft, for the tickets that send and dismiss", () => {
  const { store, drafts } = setup({ lists: { s1: makeList(1, makeInterview()) } });
  store.choose("iv-1", 0, "0");
  store.drop("iv-1");
  assert.deepEqual(progressOf(store), { answered: 0, total: 2, skipped: 2 });
  assert.equal(drafts.held["iv-1"], undefined);
});

test("an interview that arrives later gets its draft too", () => {
  const drafts = memoryDrafts({ late: [{ chosen: [0], own: false, text: "", note: "" }, { chosen: [], own: false, text: "", note: "" }] });
  const { store } = setup({ lists: { s1: makeList(0) }, drafts });
  store.receive("s1", asked(1, makeInterview({ id: "late" })));
  assert.equal(store.isChecked("late", 0, "0"), true);
});
