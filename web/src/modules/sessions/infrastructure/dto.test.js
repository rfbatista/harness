import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toChange, toCreated, toSeed, toSession, toSessionList, toStartBody } from "./dto.js";

file("sessions/infrastructure/dto");

const wire = {
  id: "s1",
  project_id: "p1",
  ticket_id: "t1",
  task: "Fix the feed",
  status: "waiting_approval",
  pending_approvals: 1,
  updated_at: "2026-10-02T14:00:00Z",
  cost_usd: 0.42,
  interactive: true,
  runs_on: "server",
};

test("maps the wire format to a frozen domain session", () => {
  const s = toSession(wire);
  assert.equal(s.agentId, "", "omitted agent_id is empty");
  assert.equal(s.ticketId, "t1");
  assert.deepEqual([s.interactive, s.runsOn], [true, "server"]);
  assert.deepEqual([toSession({ ...wire, interactive: undefined, runs_on: undefined }).interactive, toSession({ ...wire, runs_on: undefined }).runsOn], [false, ""]);
  assert.equal(toSession({ ...wire, ticket_id: undefined }).ticketId, "", "a session without a task");
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

test("the start request and the created session speak the API's shape", () => {
  assert.deepEqual(
    toStartBody({ projectId: "p1", ticketId: "t1", repositoryId: "r1", agentId: "", prompt: "go", autoAccept: "off", size: { cols: 120, rows: 32 } }),
    { project_id: "p1", ticket_id: "t1", repository_id: "r1", agent_id: undefined, prompt: "go", auto_accept: "off", runs_on: "server", size: { cols: 120, rows: 32 } },
  );
  assert.equal(
    toStartBody({ projectId: "p1", ticketId: "t1", repositoryId: "r1", agentId: "", mode: "architect", prompt: "", autoAccept: "off" }).mode,
    "architect",
  );
  assert.equal(toSession({ ...wire, mode: "architect" }).mode, "architect");
  assert.equal(toSession(wire).mode, "");
  assert.equal(toCreated({ session: wire }).id, "s1");
  assert.throws(() => toCreated({}), Codes.BAD_RESPONSE);
});

test("the seed carries agent names for live updates", () => {
  const seed = toSeed({ project_id: "p1", ticket_id: "t1", sessions: [wire], agent_names: { a1: "Reviewer" } });
  assert.deepEqual(seed.agentNames, { a1: "Reviewer" });
  assert.deepEqual(toSeed({ project_id: "p1", sessions: [] }).agentNames, {});
});

test("feed lines become upserts or deletions", () => {
  assert.equal(toChange({ session: wire }).kind, "upsert");
  assert.deepEqual(toChange({ session: { id: "s1" }, deleted: true }), { kind: "deleted", id: "s1" });
});
