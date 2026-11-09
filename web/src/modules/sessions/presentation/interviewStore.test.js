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

function setup({ lists = {}, gateway = fakeGateway(), drafts = memoryDrafts(), visible = () => true } = {}) {
  const prefs = memoryPrefs();
  const timers = fakeTimers();
  const store = interviewStore({ gateway, prefs, timers, drafts, visible });
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

// ── stack, history, and what the feed does to them ──────────────────────────

const changed = (seq, interview) => ({ seq, kind: "interview_changed", interview });
const closedAs = (id, state, over = {}) => makeInterview({ id, state, ended_at: "2026-11-08T14:41:00Z", ...(state === "sent" ? { delivery: "delivered", delivered_at: "2026-11-08T14:41:00Z", answers: [{ chosen: [0], text: "", note: "", skipped: false }, { chosen: [], text: "", note: "", skipped: true }] } : {}), ...over });
const cardsOf = (store, sid = "s1") => store.view(sid).cards.map((c) => [c.id, c.folded]);
const rowsOf = (store, sid = "s1") => store.view(sid).history.rows.map((r) => [r.id, r.state]);

test("a second interview is added above as a new card; the first, untouched, folds", () => {
  const { store, announce, said } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  assert.deepEqual(cardsOf(store), [["a", false]]);
  store.receive("s1", asked(2, makeInterview({ id: "b" })), announce);
  assert.deepEqual(cardsOf(store), [["b", false], ["a", true]]);
  assert.deepEqual(said, ["architect asked 2 more questions"]);
  assert.equal(store.view("s1").cards[1].foldedBadge, "0 of 2");
});

test("the first interview stays open when the person had touched it, and the new one is added above it", () => {
  const { store, announce } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.choose("a", 0, "0");
  store.receive("s1", asked(2, makeInterview({ id: "b" })), announce);
  assert.deepEqual(cardsOf(store), [["b", false], ["a", false]]);
  assert.equal(store.view("s1").cards[1].foldedBadge, "1 of 2");
});

test("an interview whose answers were only a note counts as touched too", () => {
  const { store, announce } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.note("a", 1, "a thought");
  store.receive("s1", asked(2, makeInterview({ id: "b" })), announce);
  assert.deepEqual(cardsOf(store), [["b", false], ["a", false]]);
});

test("each open card folds and expands on its own, whatever the order says", () => {
  const { store } = setup({ lists: { s1: makeList(2, makeInterview({ id: "a" }), makeInterview({ id: "b" })) } });
  assert.deepEqual(cardsOf(store), [["b", false], ["a", true]]);
  store.toggleCard("a");
  assert.deepEqual(cardsOf(store), [["b", false], ["a", false]]);
  store.toggleCard("b");
  assert.deepEqual(cardsOf(store), [["b", true], ["a", false]]);
  store.toggleCard("a");
  assert.deepEqual(cardsOf(store), [["b", true], ["a", true]], "both can be folded");
});

test("when the newest is sent, the next one takes its place, expanded", () => {
  const { store } = setup({ lists: { s1: makeList(2, makeInterview({ id: "a" }), makeInterview({ id: "b" })) } });
  store.receive("s1", changed(3, closedAs("b", "sent")));
  assert.deepEqual(cardsOf(store), [["a", false]]);
});

test("an interview changed to sent by the feed moves to Earlier, drops the unsent draft, and says so", () => {
  const { store, drafts, announce, said } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" }), makeInterview({ id: "b" })) } });
  store.choose("a", 0, "1");
  store.receive("s1", changed(2, closedAs("a", "sent")), announce);
  assert.deepEqual(cardsOf(store), [["b", false]]);
  assert.deepEqual(rowsOf(store), [["a", "sent"]]);
  assert.equal(store.view("s1").history.label, "Earlier · 1");
  assert.equal(drafts.held.a, undefined);
  assert.ok(drafts.dropped.includes("a"));
  assert.equal(store.answers.a, undefined);
  assert.deepEqual(said, ["Interview answered in another tab"]);
});

test("dismissed from another tab: Earlier, draft dropped, the dismissal announced", () => {
  const { store, drafts, announce, said } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.choose("a", 0, "0");
  store.receive("s1", changed(2, closedAs("a", "dismissed")), announce);
  assert.deepEqual(rowsOf(store), [["a", "dismissed"]]);
  assert.equal(drafts.held.a, undefined);
  assert.deepEqual(said, ["Interview dismissed in another tab"]);
});

test("an expired interview keeps what was typed to be read back and copied, and says the session ended", () => {
  const { store, drafts, announce, said } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.choose("a", 0, "1");
  store.type("a", 1, "in the page seed");
  store.receive("s1", changed(2, closedAs("a", "expired")), announce);
  assert.deepEqual(rowsOf(store), [["a", "expired"]]);
  assert.ok(drafts.held.a, "the draft stays in this browser");
  const row = store.view("s1").history.rows[0];
  assert.deepEqual(row.questions.map((q) => q.answer), ["The harness database", "in the page seed"]);
  assert.equal(row.copy, true);
  assert.deepEqual(said, ["Interview expired: the session ended"]);
  assert.equal(store.copyText("a").startsWith("1. Which store holds interviews?\nThe harness database"), true);
});

test("an expired interview that came with the seed reads back its draft after a reload", () => {
  const draft = [{ chosen: [0], own: false, text: "", note: "" }, { chosen: [], own: false, text: "typed before the end", note: "" }];
  const { store } = setup({ lists: { s1: makeList(1, closedAs("x", "expired")) }, drafts: memoryDrafts({ x: draft }) });
  assert.deepEqual(store.view("s1").history.rows[0].questions.map((q) => q.answer), ["A file per interview", "typed before the end"]);
});

test("an interview moved to Earlier here is not announced as another tab when the server's echo arrives", () => {
  const { store, announce, said } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.moveToEarlier("s1", closedAs("a", "sent", { delivery: "queued", delivered_at: null }));
  assert.deepEqual(rowsOf(store), [["a", "sent"]]);
  assert.equal(store.list("s1").seq, 1, "the feed's numbering is the server's");
  store.receive("s1", changed(2, closedAs("a", "sent")), announce);
  assert.deepEqual(said, []);
  assert.equal(store.view("s1").history.rows[0].sub.endsWith("delivered " + clockOf("2026-11-08T14:41:00Z")), true, "the server's copy replaces the local one");
});

const clockOf = (iso) => new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false });

test("history rows open and close on their own", () => {
  const { store } = setup({ lists: { s1: makeList(1, closedAs("a", "sent"), closedAs("b", "dismissed", { ended_at: "2026-11-08T13:00:00Z" })) } });
  assert.deepEqual(store.view("s1").history.rows.map((r) => [r.id, r.opened]), [["a", false], ["b", false]]);
  store.toggleRow("b");
  assert.deepEqual(store.view("s1").history.rows.map((r) => [r.id, r.opened]), [["a", false], ["b", true]]);
  store.toggleRow("b");
  assert.equal(store.view("s1").history.rows[1].opened, false);
});

test("with only history the toggle is there without a badge, and shows the panel; it is not shown until asked", () => {
  const { store } = setup({ lists: { s1: makeList(1, closedAs("a", "sent")), s2: makeList(0) } });
  assert.equal(store.toggle("s2"), null);
  assert.deepEqual(store.toggle("s1"), { count: "", label: "Questions", pressed: false, signal: false });
  assert.equal(store.shown("s1"), false);
  store.flip("s1");
  assert.equal(store.shown("s1"), true);
  assert.equal(store.toggle("s1").pressed, true);
  store.flip("s1");
  assert.equal(store.shown("s1"), false);
});

test("narrow, the toggle opens the history as the sheet, and there is no peek strip for it", () => {
  const { store } = setup({ lists: { s1: makeList(1, closedAs("a", "sent")) } });
  store.setDocked(false);
  assert.equal(store.peeking("s1"), false);
  store.flip("s1");
  assert.equal(store.shown("s1"), true);
});

test("when the last open interview is sent in another tab, a panel that was showing stays, now holding Earlier", () => {
  const { store, announce } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  assert.equal(store.shown("s1"), true);
  store.receive("s1", changed(2, closedAs("a", "sent")), announce);
  assert.equal(store.shown("s1"), true);
  assert.equal(store.view("s1").empty, true);
  assert.deepEqual(store.toggle("s1"), { count: "", label: "Questions", pressed: true, signal: false });
});

test("a panel the person hid stays hidden when the last interview closes", () => {
  const { store, announce } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })) } });
  store.flip("s1");
  store.receive("s1", changed(2, closedAs("a", "sent")), announce);
  assert.equal(store.shown("s1"), false);
});

test("a gap reads the list again and the panel matches the server, Earlier included", async () => {
  const gateway = fakeGateway({ s1: makeList(9, closedAs("a", "sent"), makeInterview({ id: "n" }), closedAs("o", "expired", { ended_at: "2026-11-08T10:00:00Z" })) });
  const { store, announce, said, drafts } = setup({ lists: { s1: makeList(3, makeInterview({ id: "a" })) }, gateway });
  store.choose("a", 0, "0");
  store.receive("s1", changed(8, closedAs("a", "sent")), announce);
  await flush();
  assert.deepEqual(gateway.reads, ["s1"]);
  assert.deepEqual(cardsOf(store), [["n", false]]);
  assert.deepEqual(rowsOf(store), [["a", "sent"], ["o", "expired"]]);
  assert.ok(drafts.dropped.includes("a"), "the interview that closed in what was missed loses its draft");
  assert.deepEqual(said, ["architect asked 2 more questions", "Interview answered in another tab"]);
});

test("the open questions of a session are what the title and the Agent tab count", () => {
  const { store, announce } = setup({ lists: { s1: makeList(1, makeInterview({ id: "a" })), s2: makeList(0) } });
  assert.equal(store.openQuestions("s1"), 2);
  assert.equal(store.openQuestions("s2"), 0);
  assert.equal(store.openQuestions("nope"), 0);
  store.receive("s1", asked(2, makeInterview({ id: "b" })), announce);
  assert.equal(store.openQuestions("s1"), 4);
  store.receive("s1", changed(3, closedAs("a", "sent")), announce);
  assert.equal(store.openQuestions("s1"), 2);
});

test("in a hidden tab an arrival is applied with no ring and no announcement", () => {
  let visible = false;
  const { store, timers, announce, said } = setup({ lists: { s1: makeList(2, makeInterview()) }, visible: () => visible });
  store.flip("s1");
  store.receive("s1", asked(3, makeInterview({ id: "b" })), announce);
  timers.run(0);
  assert.equal(store.toggle("s1").signal, false);
  assert.deepEqual(said, []);
  assert.equal(store.toggle("s1").count, "4", "the count is current when the tab comes back");
  visible = true;
  store.receive("s1", asked(4, makeInterview({ id: "c" })), announce);
  timers.run(0);
  assert.equal(store.toggle("s1").signal, true);
});
