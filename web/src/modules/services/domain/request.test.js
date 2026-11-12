import { assert, file, test } from "../../../shared/testing/test.js";
import { createdByLabel, isPending, pendingCount, pendingLabel, requestCard, requestsLabel, stripValues, upsertRequest } from "./request.js";

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

const NOW = new Date("2026-11-07T15:03:00Z");
const base = (o = {}) => ({
  id: "q1", projectId: "p1", serviceId: "s1", serviceName: "api", action: "update", sessionId: "abcdef12-3456", taskId: "t1", agent: "go-developer",
  reason: "", summary: "update command service api", proposed: null, changes: [], env: [], worktree: "", state: "pending", note: "", resultError: "",
  requestedAt: "2026-11-07T15:00:00Z", answeredAt: "", ...o,
});

test("a card reads as a service request: the action and service, who asks, how long ago, and where to find the session", () => {
  const card = requestCard(base({ reason: "the port clashes" }), { now: NOW, projectId: "p1" });
  assert.deepEqual([card.id, card.anchor, card.title, card.summary], ["q1", "service-request-q1", "Change api", "update command service api"]);
  assert.deepEqual([card.agentLine, card.age, card.sessionHref, card.hasSessionLink], ["go-developer · session abcdef12", "3m ago", "/projects/p1/tasks/t1?session=abcdef12-3456", true]);
  assert.deepEqual([card.reason, card.hasReason], ["the port clashes", true]);
  assert.equal(requestCard(base(), { now: NOW }).hasReason, false);
  assert.equal(requestCard(base({ taskId: "" }), { now: NOW, projectId: "p1" }).hasSessionLink, false);
  assert.deepEqual(["create", "update", "start", "restart", "stop"].map((action) => requestCard(base({ action }), { now: NOW }).title), ["Create api", "Change api", "Start api", "Restart api", "Stop api"]);
});

test("a create request shows the whole proposed definition", () => {
  const proposed = { name: "api", kind: "command", repository_id: "r1", export_repository_env: true, command: { line: "npm run dev", subdir: "web", ports: [3000, 3001] }, image: null };
  const card = requestCard(base({ action: "create", serviceId: "", proposed }), { now: NOW });
  assert.deepEqual(card.definition.map((r) => [r.label, r.value]), [["Kind", "command"], ["Command line", "npm run dev"], ["Subfolder", "web"], ["Ports", "3000, 3001"], ["Repository .env", "exported into the run"]]);
  const image = requestCard(base({ action: "create", proposed: { name: "db", kind: "image", image: { ref: "postgres:16", args: ["-c", "log_statement=all"], ports: [{ host: 5432, container: 5432, protocol: "tcp" }, { host: 53, container: 5353, protocol: "udp" }] }, command: null, export_repository_env: false } }), { now: NOW });
  assert.deepEqual(image.definition.map((r) => [r.label, r.value]), [["Kind", "image"], ["Image", "postgres:16"], ["Arguments", "-c log_statement=all"], ["Image ports", "5432:5432/tcp, 53:5353/udp"]]);
  assert.equal(requestCard(base({ action: "update" }), { now: NOW }).hasDefinition, false);
});

test("an update request shows each changed field as before and after, its variables apart", () => {
  const card = requestCard(base({ changes: [{ field: "command.line", before: "make dev", after: "npm run dev" }, { field: "command.ports", before: "8080", after: "3000" }, { field: "env.PORT", before: "", after: "" }, { field: "name", before: "api", after: "web" }] }), { now: NOW });
  assert.deepEqual(card.changes.map((c) => [c.label, c.before, c.after]), [["Command line", "make dev", "npm run dev"], ["Ports", "8080", "3000"], ["name", "api", "web"]]);
  assert.equal(card.hasChanges, true);
  assert.equal(requestCard(base({ changes: [{ field: "env.PORT", before: "", after: "" }] }), { now: NOW }).hasChanges, false);
});

test("start and restart show where the run runs and its overrides", () => {
  assert.equal(requestCard(base({ action: "start", worktree: "main" }), { now: NOW }).worktreeLine, "Runs in the main checkout.");
  assert.equal(requestCard(base({ action: "restart", worktree: "own" }), { now: NOW }).worktreeLine, "Runs in the agent's own worktree.");
  assert.equal(requestCard(base({ action: "stop" }), { now: NOW }).worktreeLine, "");
  const card = requestCard(base({ action: "start", worktree: "main", env: [{ name: "LOG", value: "debug", change: "set", previousSet: false }] }), { now: NOW });
  assert.deepEqual([card.variablesTitle, card.variables[0].changeText], ["Run overrides", "set"]);
  assert.equal(requestCard(base(), { now: NOW }).variablesTitle, "Variables");
});

test("variables show what changes, masked with a fixed mask until revealed", () => {
  const env = [
    { name: "PORT", value: "8080", change: "set", previousSet: true },
    { name: "TOKEN", value: "s3cret", change: "set", previousSet: false },
    { name: "OLD", value: "", change: "unset", previousSet: true },
  ];
  const card = requestCard(base({ env }), { now: NOW });
  assert.deepEqual(card.variables.map((v) => [v.name, v.changeText]), [["PORT", "changed"], ["TOKEN", "added"], ["OLD", "removed"]]);
  assert.equal(card.variables[0].shown, card.variables[1].shown, "the mask does not show a value's length");
  assert.equal(JSON.stringify(card).includes("s3cret"), false);
  assert.equal(card.variables[2].hasValue, false, "an unset variable has no value to reveal");
  const open = requestCard(base({ env }), { now: NOW, revealed: { TOKEN: true } });
  assert.deepEqual([open.variables[1].shown, open.variables[1].masked, open.variables[1].toggleName, open.variables[0].masked], ["s3cret", false, "Hide TOKEN", true]);
  assert.equal(card.allLabel, "Reveal all");
  assert.equal(requestCard(base({ env }), { now: NOW, revealed: { PORT: true, TOKEN: true } }).allLabel, "Hide all", "every variable that has a value");
});

test("an answered card says what came of it: done, failed with its reason, denied with the note, expired", () => {
  const card = (o) => requestCard(base(o), { now: NOW });
  assert.deepEqual([card({}).pending, card({}).outcome], [true, null]);
  assert.deepEqual([card({ state: "done" }).pending, card({ state: "done" }).outcome], [false, { text: "Approved and done.", tone: "signal" }]);
  assert.deepEqual(card({ state: "failed", resultError: "port 8080 is in use" }).outcome, { text: "Approved, but it failed: port 8080 is in use", tone: "danger" });
  assert.deepEqual(card({ state: "denied", note: "not now" }).outcome, { text: "Denied. Your note: not now", tone: "" });
  assert.deepEqual(card({ state: "denied" }).outcome, { text: "Denied.", tone: "" });
  assert.deepEqual(card({ state: "expired" }).outcome, { text: "Expired: its session ended before you answered.", tone: "attention" });
  assert.deepEqual(card({ state: "approved" }).outcome, { text: "Approved. Doing it now…", tone: "signal" });
});
