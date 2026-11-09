import { assert, file, test } from "../../../shared/testing/test.js";
import { oldestPendingBySession } from "./request.js";

file("branchrequests/domain/request");

const request = (id, sessionId, state, at) => ({ id, sessionId, state, requestedAt: new Date(at) });

test("a session's pending approval leads to its oldest pending branch request", () => {
  const list = [
    request("r3", "s1", "pending", "2026-11-09T10:03:00Z"),
    request("r2", "s2", "pending", "2026-11-09T10:02:00Z"),
    request("r1", "s1", "pending", "2026-11-09T10:01:00Z"),
  ];
  assert.deepEqual(oldestPendingBySession(list), { s1: "r1", s2: "r2" });
});

test("an answered or expired request leads nowhere", () => {
  const list = [request("r1", "s1", "approved", "2026-11-09T10:01:00Z"), request("r2", "s2", "expired", "2026-11-09T10:02:00Z")];
  assert.deepEqual(oldestPendingBySession(list), {});
});
