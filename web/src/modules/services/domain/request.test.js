import { assert, file, test } from "../../../shared/testing/test.js";
import { createdByLabel, isPending, pendingCount, pendingLabel, requestsLabel, stripValues, upsertRequest } from "./request.js";

file("services/domain/request");

const req = (o = {}) => ({ id: "q1", state: "pending", requestedAt: "2026-11-07T15:00:00Z", env: [], proposed: null, ...o });

test("only a pending request waits on the person", () => {
  assert.deepEqual(["pending", "approved", "denied", "expired", "done", "failed"].map((state) => isPending(req({ state }))), [true, false, false, false, false, false]);
  assert.equal(pendingCount([req(), req({ id: "q2", state: "done" }), req({ id: "q3" })]), 2);
});

test("a request joins the list in place of its older self, newest first", () => {
  const list = upsertRequest([req({ id: "a", requestedAt: "2026-11-07T10:00:00Z" })], req({ id: "b", requestedAt: "2026-11-07T11:00:00Z" }));
  assert.deepEqual(list.map((r) => r.id), ["b", "a"]);
  const answered = upsertRequest(list, req({ id: "a", requestedAt: "2026-11-07T10:00:00Z", state: "done" }));
  assert.deepEqual(answered.map((r) => [r.id, r.state]), [["b", "pending"], ["a", "done"]]);
});

test("values are taken off a request that is kept without opening it", () => {
  const kept = stripValues(req({ env: [{ name: "PORT", value: "8080", change: "set", previousSet: false }], proposed: { name: "api", env: "PORT=8080", command: { line: "x" } } }));
  assert.deepEqual(kept.env, [{ name: "PORT", value: "", change: "set", previousSet: false }]);
  assert.equal("env" in kept.proposed, false);
  assert.equal(kept.proposed.name, "api");
  assert.equal(stripValues(req()).proposed, null);
});

test("the words for who created a service and for the requests that wait", () => {
  assert.equal(createdByLabel({ createdBy: { kind: "session", agent: "go-developer" } }), "created by go-developer");
  assert.equal(createdByLabel({ createdBy: { kind: "session", agent: "" } }), "created by an agent");
  assert.equal(createdByLabel({ createdBy: { kind: "person", agent: "" } }), "");
  assert.deepEqual([pendingLabel(0), pendingLabel(1), pendingLabel(3)], ["", "1 request", "3 requests"]);
  assert.equal(requestsLabel(2), "Requests, 2 waiting on you");
});
