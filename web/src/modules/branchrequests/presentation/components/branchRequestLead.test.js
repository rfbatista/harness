import { assert, file, test } from "../../../../shared/testing/test.js";
import { branchRequestLead } from "./branchRequestLead.js";

file("branchrequests/presentation/components/branchRequestLead");

function lead(pendingBySession, sessionId = "s1") {
  const sent = [];
  const instance = branchRequestLead({ store: { pendingBySession } })(sessionId);
  instance.$dispatch = (name, detail) => sent.push({ name, detail });
  return { instance, sent };
}

test("a session with a pending branch request has an approvals button that opens it", () => {
  const { instance, sent } = lead({ s1: "r1" });
  assert.equal(instance.actionable("approval"), true);
  assert.equal(instance.plain("approval"), false);
  assert.equal(instance.describe("approval", "2 approvals pending"), "2 approvals pending: show the branch request waiting on you");
  instance.open("approval");
  assert.deepEqual(sent, [{ name: "branch-request-show", detail: { sessionId: "s1" } }]);
});

test("a session waiting only on a tool approval keeps plain text", () => {
  const { instance, sent } = lead({ s2: "r2" });
  assert.equal(instance.actionable("approval"), false);
  assert.equal(instance.plain("approval"), true);
  instance.open("approval");
  assert.deepEqual(sent, []);
});

test("only the approvals text leads to a request, never a report's lead", () => {
  const { instance, sent } = lead({ s1: "r1" });
  assert.equal(instance.plain("blocked"), true);
  instance.open("blocked");
  assert.deepEqual(sent, []);
});
