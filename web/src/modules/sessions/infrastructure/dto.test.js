import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toChange, toCreated, toResumeBody, toSeed, toSession, toSessionList, toStartBody, toStatusCheck } from "./dto.js";

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
  // The sessions gateway drops what toChange cannot read: an artifact change is not a session's.
  assert.throws(() => toChange({ artifact: { id: "a1", kind: "page", revision: 1, scope: "project", attached_ticket_ids: ["t2"], updated_at: "2026-10-07T12:00:00Z" } }), Codes.BAD_RESPONSE);
  assert.throws(() => toChange({ artifact: { id: "a1" }, deleted: true }), Codes.BAD_RESPONSE);
});

test("a session says whether it can be resumed, and why not", () => {
  const ended = toSession({ ...wire, status: "stopped", resumable: true, resume_blocked: "" });
  assert.deepEqual([ended.resumable, ended.resumeBlocked], [true, ""]);
  const gone = toSession({ ...wire, status: "done", resumable: false, resume_blocked: "WORKSPACE_MISSING" });
  assert.deepEqual([gone.resumable, gone.resumeBlocked], [false, "WORKSPACE_MISSING"]);
  const older = toSession(wire);
  assert.deepEqual([older.resumable, older.resumeBlocked], [false, ""], "a server without the fields: not resumable, no reason");
  const odd = toSession({ ...wire, resumable: "yes", resume_blocked: 42 });
  assert.deepEqual([odd.resumable, odd.resumeBlocked], [false, ""], "anything but true / a string reads as the default");
});

test("the resume request always runs on the server", () => {
  assert.deepEqual(toResumeBody("s1", { cols: 120, rows: 32 }), { session_id: "s1", runs_on: "server", size: { cols: 120, rows: 32 } });
  assert.deepEqual(toResumeBody("s1"), { session_id: "s1", runs_on: "server", size: undefined });
});

const check = {
  task_id: "t1",
  architect_session_id: "arch",
  delegate_session_id: "s1",
  every_minutes: 10,
  next_at: "2026-10-02T14:10:00Z",
  last_fired_at: "2026-10-02T14:00:00Z",
  fired_count: 3,
  state: "active",
};

test("a session reads its role, its task's architect and its status check", () => {
  const s = toSession({ ...wire, role: "delegate", architect_session_id: "arch", status_check: check });
  assert.equal(s.role, "delegate");
  assert.equal(s.architectSessionId, "arch");
  assert.deepEqual(s.statusCheck, {
    taskId: "t1",
    architectSessionId: "arch",
    delegateSessionId: "s1",
    everyMinutes: 10,
    nextAt: new Date("2026-10-02T14:10:00Z"),
    lastFiredAt: new Date("2026-10-02T14:00:00Z"),
    firedCount: 3,
    state: "active",
  });
});

test("a session without the role fields is a peer on a task without an architect", () => {
  const s = toSession(wire);
  assert.deepEqual([s.role, s.architectSessionId, s.statusCheck], ["", "", null]);
  const nulls = toSession({ ...wire, role: "", architect_session_id: null, status_check: null });
  assert.deepEqual([nulls.role, nulls.architectSessionId, nulls.statusCheck], ["", "", null]);
});

test("a status check without its times reads them as null; a paused one has no interval", () => {
  const paused = toStatusCheck({ ...check, every_minutes: 0, next_at: undefined, last_fired_at: undefined, fired_count: 0, state: "paused" });
  assert.deepEqual([paused.everyMinutes, paused.nextAt, paused.lastFiredAt, paused.state], [0, null, null, "paused"]);
});

test("an unknown role or a malformed status check is BAD_RESPONSE", () => {
  assert.throws(() => toSession({ ...wire, role: "overlord" }), Codes.BAD_RESPONSE);
  assert.throws(() => toStatusCheck({ ...check, state: "sleeping" }), Codes.BAD_RESPONSE);
  assert.throws(() => toStatusCheck({ ...check, delegate_session_id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toStatusCheck({ ...check, next_at: "soon" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSession({ ...wire, status_check: { ...check, every_minutes: "ten" } }), Codes.BAD_RESPONSE);
});
