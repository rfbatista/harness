import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList } from "../testing/interview-fixtures.js";
import { applyEvent, arrived, closedNow, DOCK_MIN_REM, dockedAt, emptyList, historyOf, openOnes, openQuestionCount } from "./interviews.js";

file("sessions/domain/interviews");

const asked = (seq, interview) => ({ seq, kind: "interview_asked", interview });
const changed = (seq, interview) => ({ seq, kind: "interview_changed", interview });

test("the next event is applied: a new interview is added, a changed one replaces its own by id", () => {
  let list = applyEvent(makeList(4), asked(5, makeInterview({ id: "a" })));
  assert.equal(list.outcome, "applied");
  assert.equal(list.list.seq, 5);
  assert.deepEqual(list.list.interviews.map((i) => i.id), ["a"]);
  list = applyEvent(list.list, asked(6, makeInterview({ id: "b" })));
  list = applyEvent(list.list, changed(7, makeInterview({ id: "a", state: "sent", ended_at: "2026-11-08T14:40:00Z", delivery: "queued" })));
  assert.equal(list.outcome, "applied");
  assert.deepEqual(list.list.interviews.map((i) => `${i.id}:${i.state}`), ["a:sent", "b:open"]);
});

test("an event at or before the list's seq is stale: the seed already holds it", () => {
  const list = makeList(5, makeInterview({ id: "a" }));
  assert.equal(applyEvent(list, asked(5, makeInterview({ id: "a" }))).outcome, "stale");
  assert.equal(applyEvent(list, asked(3, makeInterview({ id: "z" }))).outcome, "stale");
});

test("an event past the next one is a gap: the list has to be read again", () => {
  assert.equal(applyEvent(makeList(5), asked(7, makeInterview())).outcome, "gap");
  assert.equal(applyEvent(emptyList, asked(1, makeInterview())).outcome, "applied");
});

test("open questions are counted across the open interviews only", () => {
  const list = makeList(3, makeInterview({ id: "a" }), makeInterview({ id: "b", state: "sent" }), makeInterview({ id: "c" }));
  assert.deepEqual(openOnes(list).map((i) => i.id), ["a", "c"]);
  assert.equal(openQuestionCount(list), 4);
  assert.equal(openQuestionCount(emptyList), 0);
});

test("what arrived is the open interviews the earlier list did not have open", () => {
  const before = makeList(1, makeInterview({ id: "a" }));
  const next = makeList(2, makeInterview({ id: "a" }), makeInterview({ id: "b" }), makeInterview({ id: "c", state: "expired" }));
  assert.deepEqual(arrived(before, next).map((i) => i.id), ["b"]);
  assert.deepEqual(arrived(next, next), []);
});

test("the panel docks from 56rem of session column, whatever the rem is", () => {
  assert.equal(DOCK_MIN_REM, 56);
  assert.equal(dockedAt(896, 16), true);
  assert.equal(dockedAt(895, 16), false);
  assert.equal(dockedAt(1000, 20), false, "a larger rem is a narrower column in rem");
});

const ended = (id, state, at) => makeInterview({ id, state, ended_at: at });

test("the history is the interviews that are not open, the most recently ended first", () => {
  const list = makeList(
    6,
    makeInterview({ id: "open" }),
    ended("old", "sent", "2026-11-08T12:00:00Z"),
    ended("new", "expired", "2026-11-08T15:00:00Z"),
    ended("mid", "dismissed", "2026-11-08T13:00:00Z"),
  );
  assert.deepEqual(historyOf(list).map((i) => i.id), ["new", "mid", "old"]);
  assert.deepEqual(historyOf(makeList(1, makeInterview())), []);
});

test("an interview with no end time sorts after the ended ones, the most recently asked first", () => {
  const list = makeList(3, ended("sent", "sent", "2026-11-08T12:00:00Z"), makeInterview({ id: "b", state: "expired", ended_at: null, asked_at: "2026-11-08T10:00:00Z" }), makeInterview({ id: "c", state: "expired", ended_at: null, asked_at: "2026-11-08T11:00:00Z" }));
  assert.deepEqual(historyOf(list).map((i) => i.id), ["sent", "c", "b"]);
});

test("what closed is the interviews that were open and no longer are", () => {
  const before = makeList(2, makeInterview({ id: "a" }), makeInterview({ id: "b" }), ended("c", "sent", "2026-11-08T12:00:00Z"));
  const next = makeList(3, ended("a", "sent", "2026-11-08T14:00:00Z"), makeInterview({ id: "b" }), ended("c", "sent", "2026-11-08T12:00:00Z"), ended("d", "expired", "2026-11-08T14:00:00Z"));
  assert.deepEqual(closedNow(before, next).map((i) => [i.id, i.state]), [["a", "sent"]], "d was never open here");
  assert.deepEqual(closedNow(next, next), []);
});
