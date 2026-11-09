import { assert, file, test } from "../../../shared/testing/test.js";
import {
  applyEvent, groupServices, isActive, overrideMark, portLinks, runningCount, startedByView, statusView, timeFact, whereLabel,
} from "./service.js";

file("services/domain/service");

const NOW = new Date("2026-11-07T16:00:00Z");
const ago = (ms) => new Date(NOW.getTime() - ms).toISOString();
const run = (o = {}) => ({
  id: "r1", started_at: ago(0), ended_at: "", exit_code: null, error: "",
  started_by: { kind: "person", session_id: "", agent: "" },
  where: { kind: "main", dir: "/repo", branch: "main", session_id: "" }, sandboxed: true, ...o,
});
const svc = (o = {}) => ({
  id: "s1", projectId: "p1", name: "api", kind: "command", repositoryId: "", exportRepositoryEnv: false, env: "",
  command: { line: "make air", subdir: "", ports: [] }, image: null, status: "stopped", run: null, ...o,
});

test("every status is a dot state plus a word, and a non-zero exit is flagged", () => {
  assert.deepEqual(statusView(svc({ status: "stopped" })), { state: "idle", word: "stopped", tone: "" });
  assert.deepEqual(statusView(svc({ status: "starting" })), { state: "reconnecting", word: "starting", tone: "" });
  assert.deepEqual(statusView(svc({ status: "running" })), { state: "running", word: "running", tone: "" });
  assert.deepEqual(statusView(svc({ status: "exited", run: run({ exit_code: 0 }) })), { state: "done", word: "exited · code 0", tone: "" });
  assert.deepEqual(statusView(svc({ status: "exited", run: run({ exit_code: 1 }) })), { state: "done", word: "exited · code 1", tone: "attention" });
  assert.deepEqual(statusView(svc({ status: "failed", run: run({ error: "x" }) })), { state: "failed", word: "failed", tone: "" });
});

test("starting and running count as active", () => {
  assert.deepEqual(["stopped", "starting", "running", "exited", "failed"].map((status) => isActive(svc({ status }))), [false, true, true, false, false]);
});

test("the list is grouped Running (starting included) and Not running, each by name, and an empty group is left out", () => {
  const list = [svc({ id: "1", name: "web", status: "running" }), svc({ id: "2", name: "db", status: "stopped" }), svc({ id: "3", name: "api", status: "starting" }), svc({ id: "4", name: "cache", status: "failed" })];
  const groups = groupServices(list);
  assert.deepEqual(groups.map((g) => [g.key, g.label, g.services.map((s) => s.name)]), [["running", "Running", ["api", "web"]], ["stopped", "Not running", ["cache", "db"]]]);
  assert.deepEqual(groupServices([list[1]]).map((g) => g.key), ["stopped"]);
  assert.deepEqual(groupServices([]), []);
});

test("the running count is the starting and running services", () => {
  assert.equal(runningCount([svc({ status: "running" }), svc({ status: "starting" }), svc({ status: "exited" })]), 2);
});

test("time facts: up, starting for, ran and ended, stopped, never run", () => {
  assert.equal(timeFact(svc({ status: "running", run: run({ started_at: ago(2 * 3_600_000 + 14 * 60_000) }) }), NOW), "up 2h 14m");
  assert.equal(timeFact(svc({ status: "starting", run: run({ started_at: ago(6_000) }) }), NOW), "starting for 6s");
  assert.equal(timeFact(svc({ status: "exited", run: run({ started_at: ago(4 * 60_000 + 38_000), ended_at: ago(4 * 60_000), exit_code: 0 }) }), NOW), "ran 38s · ended 4m ago");
  assert.equal(timeFact(svc({ status: "stopped", run: run({ started_at: ago(3_600_000), ended_at: ago(20 * 60_000) }) }), NOW), "stopped 20m ago");
  assert.equal(timeFact(svc({ status: "stopped" }), NOW), "never run");
});

test("a command service's ports link to localhost; an image's link on the host port and show a different container port", () => {
  const cmd = portLinks(svc({ name: "api", command: { line: "x", subdir: "", ports: [8080] } }));
  assert.deepEqual(cmd, [{ text: ":8080", href: "http://localhost:8080", name: "Open localhost:8080 (api)" }]);
  const img = portLinks(svc({
    name: "db", kind: "image", command: null,
    image: { ref: "postgres:16", args: [], ports: [{ host: 5432, container: 5432, protocol: "tcp" }, { host: 8081, container: 80, protocol: "tcp" }, { host: 53, container: 53, protocol: "udp" }] },
  }));
  assert.deepEqual(img.map((p) => [p.text, p.href]), [[":5432", "http://localhost:5432"], [":8081 → 80", "http://localhost:8081"], [":53/udp", ""]]);
});

test("where a run runs, in words", () => {
  assert.equal(whereLabel(run()), "main checkout @ main");
  assert.equal(whereLabel(run({ where: { kind: "session", dir: "/w", branch: "agent/x", session_id: "s" } })), "session worktree @ agent/x");
  assert.equal(whereLabel(run({ where: { kind: "service_folder", dir: "/f", branch: "", session_id: "" } })), "service folder");
  assert.equal(whereLabel(run({ where: { kind: "container", dir: "", branch: "", session_id: "" } })), "container");
  assert.equal(whereLabel(null), "");
});

test("who started a run: you, or an agent's session", () => {
  assert.deepEqual(startedByView(run()), { text: "started by you", sessionId: "", href: "" });
  const by = (task_id) => run({ started_by: { kind: "session", session_id: "abcdef12-0000", agent: "backend", task_id } });
  assert.deepEqual(startedByView(by("t1"), "p1"), { text: "started by backend · session abcdef12", sessionId: "abcdef12-0000", href: "/projects/p1/tasks/t1?session=abcdef12-0000" });
  assert.equal(startedByView(by(""), "p1").href, "", "no task, no link: the text stands alone");
  assert.equal(startedByView(null), null);
});

const state = (seq, services = []) => ({ seq, services });
const changed = (seq, service, running = 0) => ({ seq, kind: "service_changed", service, runningCount: running });

test("an event at or below the list's seq is already in the list", () => {
  const r = applyEvent(state(5, [svc({ status: "running" })]), changed(5, svc({ status: "stopped" })));
  assert.equal(r.reload, false);
  assert.equal(r.state.services[0].status, "running");
  assert.equal(r.applied, false);
});

test("the next event replaces or adds its service, keeping the list sorted by name", () => {
  let r = applyEvent(state(1, [svc({ id: "1", name: "b" })]), changed(2, svc({ id: "2", name: "a" }), 0));
  assert.deepEqual(r.state.services.map((s) => s.name), ["a", "b"]);
  assert.equal(r.state.seq, 2);
  r = applyEvent(r.state, changed(3, svc({ id: "1", name: "b", status: "running" }), 1));
  assert.deepEqual(r.state.services.map((s) => [s.name, s.status]), [["a", "stopped"], ["b", "running"]]);
  assert.equal(r.runningCount, 1);
});

test("a deleted service leaves the list", () => {
  const r = applyEvent(state(1, [svc({ id: "1" }), svc({ id: "2", name: "b" })]), { seq: 2, kind: "service_deleted", serviceId: "1", runningCount: 0 });
  assert.deepEqual(r.state.services.map((s) => s.id), ["2"]);
});

test("a gap in seq asks for the list to be read again", () => {
  const r = applyEvent(state(1, [svc()]), changed(3, svc()));
  assert.equal(r.reload, true);
  assert.equal(r.state.seq, 1);
});

test("an event kind it does not know is ignored but keeps the sequence", () => {
  const r = applyEvent(state(1, [svc()]), { seq: 2, kind: "service_moved", runningCount: 0 });
  assert.equal(r.reload, false);
  assert.equal(r.state.seq, 2);
});

test("the navigation badge is hidden at 0, shows the count, and caps at 99+", async () => {
  const { navBadge } = await import("./service.js");
  assert.deepEqual(navBadge(0), { text: "", hidden: true });
  assert.deepEqual(navBadge(3), { text: "3", hidden: false });
  assert.deepEqual(navBadge(99), { text: "99", hidden: false });
  assert.deepEqual(navBadge(140), { text: "99+", hidden: false });
});

test("a run's overrides read as a mark naming who set them, and none as no mark", () => {
  const by = (agent) => ({ name: "X", by: `${agent} · session abcdef12` });
  assert.equal(overrideMark({ overrides: [] }), "");
  assert.equal(overrideMark(null), "");
  assert.equal(overrideMark({ overrides: [by("go-developer")] }), "1 override by go-developer");
  assert.equal(overrideMark({ overrides: [by("go-developer"), by("go-developer")] }), "2 overrides by go-developer");
  assert.equal(overrideMark({ overrides: [by("go-developer"), by("backend"), by("go-developer")] }), "3 overrides by go-developer and backend");
  assert.equal(overrideMark({ overrides: [by("a"), by("b"), by("c")] }), "3 overrides by a, b and c");
  assert.equal(overrideMark({ overrides: [{ name: "X", by: "" }] }), "1 override by an agent");
});
