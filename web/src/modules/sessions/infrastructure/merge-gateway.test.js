import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { mergeGateway } from "./merge-gateway.js";

file("sessions/infrastructure/merge-gateway");

function fakeApi(answers) {
  const calls = [];
  return {
    calls,
    api: {
      async get(path, query) {
        calls.push(["GET", path, query]);
        return answers.get;
      },
      async post(path, body) {
        calls.push(["POST", path, body]);
        if (answers.fail) throw answers.fail;
        return answers.post;
      },
    },
  };
}

test("an offer says whether a merge applies, into which branch, and what it would do", async () => {
  const { api, calls } = fakeApi({
    get: {
      available: true,
      branch: "agent/x",
      into: "develop",
      ahead: 3,
      uncommitted_changes: 2,
      suggested_message: "Add SSE feed",
      targets: [
        { name: "main", default: true, checked_out: true, dirty: false },
        { name: "develop", default: false, checked_out: false, dirty: false },
      ],
    },
  });
  const offer = await mergeGateway(api).offer("s/1", "develop");
  assert.deepEqual(
    [offer.available, offer.branch, offer.into, offer.reason, offer.ahead, offer.uncommittedChanges, offer.suggestedMessage],
    [true, "agent/x", "develop", "", 3, 2, "Add SSE feed"],
  );
  assert.deepEqual(offer.targets.map((t) => ({ ...t })), [
    { name: "main", isDefault: true, checkedOut: true, dirty: false },
    { name: "develop", isDefault: false, checkedOut: false, dirty: false },
  ]);
  assert.deepEqual(calls, [["GET", "/sessions/s%2F1/merge", { target: "develop" }]]);
  const none = await mergeGateway(fakeApi({ get: { available: false, reason: "MERGE_NO_BRANCH" } }).api).offer("s");
  assert.deepEqual([none.available, none.reason, none.targets.length], [false, "MERGE_NO_BRANCH", 0]);
});

test("an offer for no target asks for the default branch", async () => {
  const { api, calls } = fakeApi({ get: { available: true, into: "main" } });
  await mergeGateway(api).offer("s1", "");
  assert.deepEqual(calls[0][2], { target: undefined });
});

test("a merge sends the target, the method and, for a squash only, the message", async () => {
  const answer = { at: "2026-10-23T12:00:00Z", merged: true, method: "squash", into: "develop", head: "abc123" };
  const { api, calls } = fakeApi({ post: answer });
  const result = await mergeGateway(api).merge("s1", { target: "develop", method: "squash", message: "Add SSE feed" });
  assert.deepEqual([result.merged, result.method, result.into, result.head, result.at.toISOString()], [true, "squash", "develop", "abc123", "2026-10-23T12:00:00.000Z"]);
  assert.deepEqual(calls, [["POST", "/sessions/s1/merge", { target: "develop", method: "squash", message: "Add SSE feed" }]]);
  await mergeGateway(api).merge("s1", { target: "main", method: "merge", message: "ignored" });
  assert.deepEqual(calls[1][2], { target: "main", method: "merge", message: undefined });
  await mergeGateway(api).merge("s1");
  assert.deepEqual(calls[2][2], { target: undefined, method: undefined, message: undefined }, "an empty request is today's request");
});

test("a merge record from before squash existed reads as a merge commit", async () => {
  const { api } = fakeApi({ post: { at: "2026-10-23T12:00:00Z", merged: true, into: "main", head: "abc123" } });
  assert.equal((await mergeGateway(api).merge("s1")).method, "merge");
});

test("a malformed answer is a BAD_RESPONSE", async () => {
  await assert.rejects(mergeGateway(fakeApi({ get: {} }).api).offer("s"), Codes.BAD_RESPONSE);
  await assert.rejects(mergeGateway(fakeApi({ post: null }).api).merge("s"), Codes.BAD_RESPONSE);
});
