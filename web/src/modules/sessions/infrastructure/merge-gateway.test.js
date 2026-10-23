import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { mergeGateway } from "./merge-gateway.js";

file("sessions/infrastructure/merge-gateway");

function fakeApi(answers) {
  const calls = [];
  return {
    calls,
    api: {
      async get(path) {
        calls.push(["GET", path]);
        return answers.get;
      },
      async post(path) {
        calls.push(["POST", path]);
        if (answers.fail) throw answers.fail;
        return answers.post;
      },
    },
  };
}

test("an offer says whether a merge applies and into which branch", async () => {
  const { api, calls } = fakeApi({ get: { available: true, into: "main" } });
  const offer = await mergeGateway(api).offer("s/1");
  assert.deepEqual({ ...offer }, { available: true, into: "main", reason: "" });
  assert.deepEqual(calls, [["GET", "/sessions/s%2F1/merge"]]);
  const none = await mergeGateway(fakeApi({ get: { available: false, reason: "MERGE_NO_BRANCH" } }).api).offer("s");
  assert.deepEqual([none.available, none.reason], [false, "MERGE_NO_BRANCH"]);
});

test("a merge answers the new HEAD", async () => {
  const { api, calls } = fakeApi({ post: { at: "2026-10-23T12:00:00Z", merged: true, into: "main", head: "abc123" } });
  const result = await mergeGateway(api).merge("s1");
  assert.deepEqual([result.merged, result.into, result.head, result.at.toISOString()], [true, "main", "abc123", "2026-10-23T12:00:00.000Z"]);
  assert.deepEqual(calls, [["POST", "/sessions/s1/merge"]]);
});

test("a malformed answer is a BAD_RESPONSE", async () => {
  await assert.rejects(mergeGateway(fakeApi({ get: {} }).api).offer("s"), Codes.BAD_RESPONSE);
  await assert.rejects(mergeGateway(fakeApi({ post: null }).api).merge("s"), Codes.BAD_RESPONSE);
});
