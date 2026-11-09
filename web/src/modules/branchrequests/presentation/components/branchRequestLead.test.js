import { assert, file, test } from "../../../../shared/testing/test.js";
import { branchRequestLead } from "./branchRequestLead.js";

file("branchrequests/presentation/components/branchRequestLead");

function lead(pendingBySession, sessionId = "s1") {
  const sent = [];
  const instance = branchRequestLead({ store: { pendingBySession } })(sessionId);
  instance.$dispatch = (name, detail) => sent.push({ name, detail });
  const click = { stopped: false, stopPropagation() { this.stopped = true; }, preventDefault() {} };
  return { instance, sent, click };
}

test("a session with a pending branch request has an approvals text that opens it", () => {
  const { instance, sent, click } = lead({ s1: "r1" });
  assert.equal(instance.role("approval"), "button");
  assert.equal(instance.tabindex("approval"), "0");
  assert.equal(instance.actionAttr("approval"), "");
  instance.open("approval", click);
  assert.deepEqual(sent, [{ name: "branch-request-show", detail: { sessionId: "s1" } }]);
  assert.ok(click.stopped, "the click is not a row selection");
});

test("a session waiting only on a tool approval keeps plain text", () => {
  const { instance, sent, click } = lead({ s2: "r2" });
  assert.equal(instance.role("approval"), null);
  assert.equal(instance.actionAttr("approval"), null);
  instance.open("approval", click);
  assert.deepEqual(sent, []);
  assert.ok(!click.stopped, "the row still gets its click");
});

test("only the approvals text leads to a request, never a report's lead", () => {
  const { instance, sent, click } = lead({ s1: "r1" });
  assert.equal(instance.role("blocked"), null);
  instance.open("blocked", click);
  assert.deepEqual(sent, []);
});
