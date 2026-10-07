import { assert, file, test } from "../../../shared/testing/test.js";
import { makeMessage } from "../testing/channel-fixtures.js";
import { T0 } from "../testing/fixtures.js";
import { lastReportOf, mergeMessages, newestAt, threadOf, upsertMessage } from "./channel.js";

file("sessions/domain/channel");

const at = (m) => new Date(T0.getTime() + m * 60_000);

test("messages stay oldest first; one announced again replaces itself", () => {
  let list = upsertMessage([], makeMessage({ id: "b", createdAt: at(2) }));
  list = upsertMessage(list, makeMessage({ id: "a", createdAt: at(1) }));
  list = upsertMessage(list, makeMessage({ id: "b", createdAt: at(2), delivered: false }));
  assert.deepEqual(list.map((m) => `${m.id}:${m.delivered}`), ["a:true", "b:false"]);
  list = mergeMessages(list, [makeMessage({ id: "c", createdAt: at(3) }), makeMessage({ id: "a", createdAt: at(1), body: "edited" })]);
  assert.deepEqual(list.map((m) => m.id), ["a", "b", "c"]);
  assert.equal(list[0].body, "edited");
  assert.equal(newestAt(list).getTime(), at(3).getTime());
  assert.equal(newestAt([]), null);
});

test("a delegate's thread is what it sent and what it was sent", () => {
  const list = [
    makeMessage({ id: "1", fromSessionId: "d1", toSessionId: "arch" }),
    makeMessage({ id: "2", fromSessionId: "arch", toSessionId: "d2" }),
    makeMessage({ id: "3", fromSessionId: "arch", toSessionId: "d1" }),
  ];
  assert.deepEqual(threadOf(list, "d1").map((m) => m.id), ["1", "3"]);
});

test("a delegate's last report is its newest status report, not a question or a reply", () => {
  const list = [
    makeMessage({ id: "old", fromSessionId: "d1", status: "working", createdAt: at(1) }),
    makeMessage({ id: "new", fromSessionId: "d1", status: "ready_for_review", createdAt: at(2) }),
    makeMessage({ id: "q", fromSessionId: "d1", kind: "question", status: "", createdAt: at(3) }),
    makeMessage({ id: "other", fromSessionId: "d2", createdAt: at(4) }),
  ];
  assert.equal(lastReportOf(list, "d1").id, "new");
  assert.equal(lastReportOf(list, "d3"), null);
});
