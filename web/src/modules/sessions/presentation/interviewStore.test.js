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

function setup({ lists = {}, gateway = fakeGateway() } = {}) {
  const prefs = memoryPrefs();
  const timers = fakeTimers();
  const store = interviewStore({ gateway, prefs, timers });
  store.seed(lists);
  const said = [];
  return { store, gateway, prefs, timers, said, announce: (t) => said.push(t) };
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
