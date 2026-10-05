import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toChange, toSession, toSessionList } from "./dto.js";

file("sessions/infrastructure/dto");

const wire = {
  id: "s1",
  project_id: "p1",
  task: "Fix the feed",
  status: "waiting_approval",
  pending_approvals: 1,
  updated_at: "2026-10-02T14:00:00Z",
  cost_usd: 0.42,
};

test("maps the wire format to a frozen domain session", () => {
  const s = toSession(wire);
  assert.equal(s.agentId, "", "omitted agent_id is empty");
  assert.equal(s.updatedAt.toISOString(), "2026-10-02T14:00:00.000Z");
  assert.ok(Object.isFrozen(s));
  assert.equal("cost_usd" in s, false, "fields the UI does not use stay behind");
});

test("a malformed session is BAD_RESPONSE", () => {
  assert.throws(() => toSession({ ...wire, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSession({ ...wire, status: "exploded" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSession({ ...wire, updated_at: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSessionList([wire]), Codes.BAD_RESPONSE);
});

test("feed lines become upserts or deletions", () => {
  assert.equal(toChange({ session: wire }).kind, "upsert");
  assert.deepEqual(toChange({ session: { id: "s1" }, deleted: true }), { kind: "deleted", id: "s1" });
});
