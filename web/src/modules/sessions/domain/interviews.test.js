import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList } from "../testing/interview-fixtures.js";
import { applyEvent, arrived, DOCK_MIN_REM, dockedAt, emptyList, openOnes, openQuestionCount } from "./interviews.js";

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
