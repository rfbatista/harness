import { StructuredError } from "../../../../shared/domain/errors.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { branchDTO, fakeServices, listDTO, pageEl, runDTO, serviceDTO, worktreeDTO } from "../../testing/fakeGateway.js";
import { servicesPage } from "./servicesPage.js";

file("services/presentation/components/servicesPage");

const NOW = "2026-11-07T16:00:00Z";
const running = (o = {}) => serviceDTO({ status: "running", run: runDTO({ started_at: "2026-11-07T14:00:00Z" }), ...o });

function open({ seed, gateway = fakeServices(), prefs } = {}) {
  const timers = { intervals: [], setInterval: (fn) => (timers.intervals.push(fn), timers.intervals.length), clearInterval() {} };
  const el = pageEl(seed ?? { project_id: "p1", list: listDTO([running({ id: "1", name: "web" }), serviceDTO({ id: "2", name: "db" })], { seq: 4 }) });
  const { instance } = mount(servicesPage({ gateway, clock: fixedClock(NOW), timers, prefs }), { el });
  return { page: instance, gateway, timers };
}
const rowNames = (page) => page.groups.flatMap((g) => g.rows.map((r) => r.name));

test("it paints the seeded list without reading it, grouped, and follows the project's feed", () => {
  const { page, gateway } = open();
  page.init();
  assert.equal(gateway.listCalls, 0);
  assert.deepEqual(page.groups.map((g) => [g.label, g.rows.map((r) => r.name)]), [["Running", ["web"]], ["Not running", ["db"]]]);
  assert.deepEqual(gateway.followed, ["p1"]);
  assert.equal(page.loading, false);
});

test("a seed with no list reads the list, and shows the skeleton's loading state until it arrives", async () => {
  const gateway = fakeServices({ list: listDTO([serviceDTO({ name: "api" })], { seq: 2 }) });
  const { page } = open({ seed: { project_id: "p1", list: null }, gateway });
  page.init();
  assert.equal(page.loading, true);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(gateway.listCalls, 1);
  assert.equal(page.loading, false);
  assert.deepEqual(rowNames(page), ["api"]);
});

test("a list that cannot be read shows the error, and Retry reads it again", async () => {
  const gateway = fakeServices();
  gateway.listError = new StructuredError("NETWORK", "The harness server is not reachable.");
  const { page } = open({ seed: { project_id: "p1", list: null }, gateway });
  page.init();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(page.error.code, "NETWORK");
  assert.equal(page.loading, false);
  gateway.listError = null;
  await page.retry();
  assert.equal(page.error, null);
});

test("a row shows kind, the command, status, ports, who, where, time and the not sandboxed mark", () => {
  const svc = running({ id: "1", name: "api", command: { line: "make air", subdir: "", ports: [8080] }, run: runDTO({ started_at: "2026-11-07T13:46:00Z", sandboxed: false, started_by: { kind: "session", session_id: "abcdef12-xx", agent: "backend" } }) });
  const { page } = open({ seed: { project_id: "p1", list: listDTO([svc], { seq: 1 }) } });
  page.init();
  const row = page.groups[0].rows[0];
  assert.equal(row.kindLabel, "command");
  assert.equal(row.line, "make air");
  assert.deepEqual([row.status.state, row.status.word], ["running", "running"]);
  assert.deepEqual(row.ports.map((p) => p.href), ["http://localhost:8080"]);
  assert.equal(row.startedBy.text, "started by backend · session abcdef12");
  assert.equal(row.where, "main checkout @ main");
  assert.equal(row.time, "up 2h 14m");
  assert.equal(row.notSandboxed, true);
});

test("an image service shows its reference and arguments; a failed one shows its reason", () => {
  const img = serviceDTO({ id: "1", name: "db", kind: "image", command: null, image: { ref: "postgres:16", args: ["-c", "log_statement=all"], ports: [] } });
  const failed = serviceDTO({ id: "2", name: "web", status: "failed", run: runDTO({ error: "port 8080 is in use", ended_at: "2026-11-07T15:59:00Z" }) });
  const { page } = open({ seed: { project_id: "p1", list: listDTO([img, failed], { seq: 1 }) } });
  page.init();
  const [db, web] = page.groups[0].rows;
  assert.equal(db.kindLabel, "image");
  assert.equal(db.line, "postgres:16 -c log_statement=all");
  assert.equal(web.reason, "port 8080 is in use");
  assert.equal(web.status.state, "failed");
});

test("a feed event updates its row, and one at or below the list's seq is ignored", () => {
  const { page, gateway } = open();
  page.init();
  gateway.emit({ seq: 4, kind: "service_changed", service: running({ id: "2", name: "db" }), running_count: 2 });
  assert.deepEqual(page.groups.map((g) => g.key), ["running", "stopped"], "seq 4 is already in the list");
  gateway.emit({ seq: 5, kind: "service_changed", service: running({ id: "2", name: "db" }), running_count: 2 });
  assert.deepEqual(page.groups.map((g) => [g.key, g.rows.map((r) => r.name)]), [["running", ["db", "web"]]]);
  assert.equal(page.runningCount, 2);
});

test("a deleted service leaves the list and the page says so when it is empty", () => {
  const { page, gateway } = open({ seed: { project_id: "p1", list: listDTO([serviceDTO({ id: "1" })], { seq: 1 }) } });
  page.init();
  assert.equal(page.empty, false);
  gateway.emit({ seq: 2, kind: "service_deleted", service_id: "1", running_count: 0 });
  assert.equal(page.empty, true);
});

test("a gap in the feed reads the list again", async () => {
  const gateway = fakeServices({ list: listDTO([serviceDTO({ id: "9", name: "fresh" })], { seq: 9 }) });
  const { page } = open({ gateway });
  page.init();
  gateway.emit({ seq: 7, kind: "service_changed", service: running(), running_count: 1 });
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(gateway.listCalls, 1);
  assert.deepEqual(rowNames(page), ["fresh"]);
});

test("a resynced feed reads the list again; a live one does not", async () => {
  const gateway = fakeServices({ list: listDTO([serviceDTO({ name: "again" })], { seq: 6 }) });
  const { page } = open({ gateway });
  page.init();
  gateway.status("live");
  assert.equal(gateway.listCalls, 0);
  assert.equal(page.live, true);
  gateway.status("paused");
  assert.equal(page.live, false);
  gateway.status("resynced");
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(gateway.listCalls, 1);
  assert.deepEqual(rowNames(page), ["again"]);
});

test("the running count reads '3 running' and is a polite announcement for the toolbar", () => {
  const { page } = open({ seed: { project_id: "p1", list: listDTO([running({ id: "1", name: "a" }), running({ id: "2", name: "b" }), serviceDTO({ id: "3", name: "c", status: "starting" })], { seq: 1 }) } });
  page.init();
  assert.equal(page.runningLabel, "3 running");
  assert.equal(page.hasRunning, true);
});

test("times move on with the clock tick", () => {
  const { page, timers } = open();
  page.init();
  assert.equal(page.groups[0].rows[0].time, "up 2h");
  const clock = fixedClock("2026-11-07T16:30:00Z");
  page.now = clock.now();
  assert.equal(page.groups[0].rows[0].time, "up 2h 30m");
  assert.equal(timers.intervals.length, 1);
});

test("it stops following and ticking when the page goes", () => {
  const { page, gateway } = open();
  page.init();
  page.destroy();
  assert.equal(gateway.unfollowed, 1);
});

// ── Start, Stop and Restart ──────────────────────────────────────────────
const deferred = () => {
  const d = {};
  d.promise = new Promise((resolve, reject) => { d.resolve = resolve; d.reject = reject; });
  return d;
};
const tick = async () => { for (let i = 0; i < 5; i++) await Promise.resolve(); };
const openWith = (services, { runtime, sandbox, gateway, prefs } = {}) => {
  const gw = gateway ?? fakeServices({ list: listDTO(services, { seq: 1, ...(runtime ? { runtime } : {}), ...(sandbox ? { sandbox } : {}) }) });
  const { page } = open({ seed: { project_id: "p1", list: gw.list }, gateway: gw, prefs });
  page.init();
  return { page, gateway: gw };
};
const rowOf = (page, name) => page.groups.flatMap((g) => g.rows).find((r) => r.name === name);

test("a stopped service offers Start; a running one offers Stop and Restart; each name carries the service", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "db" }), running({ id: "2", name: "api" })]);
  const db = rowOf(page, "db");
  assert.deepEqual([db.start.show, db.stop.show, db.restart.show], [true, false, false]);
  assert.equal(db.start.name, "Start db");
  const api = rowOf(page, "api");
  assert.deepEqual([api.start.show, api.stop.show, api.restart.show], [false, true, true]);
  assert.deepEqual([api.stop.name, api.restart.name], ["Stop api", "Restart api"]);
});

test("a starting service offers Stop but not Restart", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "db", status: "starting" })]);
  const db = rowOf(page, "db");
  assert.deepEqual([db.stop.show, db.restart.show], [true, false]);
});

test("Start calls start_service; while it is in flight only that row's buttons are disabled and say Starting…", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "db" }), serviceDTO({ id: "2", name: "web" })]);
  const wait = deferred();
  gateway.answer.start = () => wait.promise;
  const pending = page.start("1");
  assert.deepEqual(gateway.actions, ["start:1"]);
  assert.deepEqual([rowOf(page, "db").start.disabled, rowOf(page, "db").start.busy, rowOf(page, "db").start.text], [true, true, "Starting…"]);
  assert.equal(rowOf(page, "web").start.disabled, false);
  wait.resolve({ service: toDTO(gateway, "1", "running"), alreadyRunning: false });
  await pending;
  assert.equal(rowOf(page, "db").stop.show, true);
  assert.equal(rowOf(page, "db").start.busy, false);
});

function toDTO(gateway, id, status) {
  return gateway.byId(id, status);
}

test("a second click on a row whose action is in flight starts nothing more", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "db" })]);
  const wait = deferred();
  gateway.answer.start = () => wait.promise;
  const first = page.start("1");
  await page.start("1");
  await page.stop("1");
  assert.deepEqual(gateway.actions, ["start:1"]);
  wait.resolve({ service: toDTO(gateway, "1", "running"), alreadyRunning: false });
  await first;
});

test("Stop and Restart call their endpoints and say Stopping… and Restarting…", async () => {
  const { page, gateway } = openWith([running({ id: "1", name: "api" })]);
  const wait = deferred();
  gateway.answer.stop = () => wait.promise;
  const stopping = page.stop("1");
  assert.equal(rowOf(page, "api").stop.text, "Stopping…");
  wait.resolve(toDTO(gateway, "1", "stopped"));
  await stopping;
  assert.equal(rowOf(page, "api").start.show, true);
  const again = deferred();
  gateway.answer.restart = () => again.promise;
  page.services = [toDTO(gateway, "1", "running")];
  const restarting = page.restart("1");
  assert.equal(rowOf(page, "api").restart.text, "Restarting…");
  again.resolve(toDTO(gateway, "1", "running"));
  await restarting;
  assert.deepEqual(gateway.actions, ["stop:1", "restart:1"]);
});

test("Start on a running service starts nothing: it opens the log", async () => {
  const { page, gateway } = openWith([running({ id: "1", name: "api" })]);
  await page.start("1");
  assert.deepEqual(gateway.actions, []);
  assert.equal(page.logId, "1");
});

test("when the server says it was already running, the log opens and nothing else changes", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  gateway.answer.start = async () => ({ service: toDTO(gateway, "1", "running"), alreadyRunning: true });
  await page.start("1");
  assert.equal(page.logId, "1");
  assert.equal(rowOf(page, "api").stop.show, true);
});

test("the feed's echo of my own action can arrive before the answer without flicker", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "db" })]);
  const wait = deferred();
  gateway.answer.start = () => wait.promise;
  const pending = page.start("1");
  gateway.emit({ seq: 2, kind: "service_changed", service: gateway.dtoOf("1", "running"), running_count: 1 });
  assert.equal(rowOf(page, "db").status.word, "running");
  gateway.emit({ seq: 3, kind: "service_changed", service: gateway.dtoOf("1", "exited", runDTO({ exit_code: 0, ended_at: "2026-11-07T15:30:00Z" })), running_count: 0 });
  wait.resolve({ service: toDTO(gateway, "1", "running"), alreadyRunning: false });
  await pending;
  assert.equal(rowOf(page, "db").status.word, "exited · code 0", "the older answer does not undo the newer event");
});

test("a refused Start shows the server's message with its code, and Dismiss clears it", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  gateway.answer.start = async () => { throw new StructuredError("SANDBOX_UNAVAILABLE", "The sandbox is unavailable on this machine, so nothing was started."); };
  await page.start("1");
  assert.equal(page.refusal.title, "api did not start.");
  assert.equal(page.refusal.message, "The sandbox is unavailable on this machine, so nothing was started.");
  assert.equal(page.refusal.code, "SANDBOX_UNAVAILABLE");
  assert.equal(rowOf(page, "api").start.disabled, false, "Start works again after a refusal");
  page.dismissRefusal();
  assert.equal(page.refusal, null);
});

test("a refusal stays until dismissed or the next Start", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" }), serviceDTO({ id: "2", name: "db" })]);
  gateway.answer.start = async (id) => { if (id === "1") throw new StructuredError("RUNTIME_UNAVAILABLE", "docker was not found"); return { service: toDTO(gateway, id, "running"), alreadyRunning: false }; };
  await page.start("1");
  assert.equal(page.refusal.code, "RUNTIME_UNAVAILABLE");
  await page.start("2");
  assert.equal(page.refusal, null);
});

test("a failed Stop shows its error too", async () => {
  const { page, gateway } = openWith([running({ id: "1", name: "api" })]);
  gateway.answer.stop = async () => { throw new StructuredError("SERVICE_NOT_FOUND", "no such service"); };
  await page.stop("1");
  assert.equal(page.refusal.title, "api did not stop.");
  assert.equal(page.refusal.code, "SERVICE_NOT_FOUND");
});

test("a failed run comes back as the service with status failed, not as a refusal", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  gateway.answer.start = async () => ({ service: gateway.byId("1", "failed", runDTO({ error: "port 8080 is in use" })), alreadyRunning: false });
  await page.start("1");
  assert.equal(page.refusal, null);
  assert.equal(rowOf(page, "api").reason, "port 8080 is in use");
});

test("the notices: sandbox unavailable, sandbox off, runtime not found; none when all is well", () => {
  assert.equal(openWith([], {}).page.notices.length, 0);
  assert.deepEqual(openWith([], { sandbox: "unavailable" }).page.notices.map((n) => n.key), ["sandbox-unavailable"]);
  assert.deepEqual(openWith([], { sandbox: "off" }).page.notices.map((n) => n.key), ["sandbox-off"]);
  const image = serviceDTO({ id: "9", name: "db", kind: "image", command: null, image: { ref: "postgres:16", args: [], ports: [] } });
  assert.deepEqual(openWith([serviceDTO()], { runtime: { name: "docker", available: false } }).page.notices.map((n) => n.key), [], "a project with only command services does not see it");
  const both = openWith([image], { sandbox: "off", runtime: { name: "docker", available: false } }).page.notices;
  assert.deepEqual(both.map((n) => n.key), ["sandbox-off", "runtime-missing"]);
  assert.equal(both[1].title, "docker was not found on this machine.");
  assert.equal(both[1].code, "RUNTIME_UNAVAILABLE");
});

test("with the runtime missing, Start on an image service is disabled with a reason, and a command service's is not", () => {
  const img = serviceDTO({ id: "1", name: "db", kind: "image", command: null, image: { ref: "postgres:16", args: [], ports: [] } });
  const cmd = serviceDTO({ id: "2", name: "api" });
  const { page } = openWith([img, cmd], { runtime: { name: "docker", available: false } });
  assert.equal(rowOf(page, "db").start.disabled, true);
  assert.equal(rowOf(page, "db").start.title, "docker was not found. Image services cannot start.");
  assert.equal(rowOf(page, "api").start.disabled, false);
});

test("with the sandbox unavailable, Start on a command service stays enabled: the server refuses and the banner explains", () => {
  const { page } = openWith([serviceDTO({ id: "2", name: "api" })], { sandbox: "unavailable" });
  assert.equal(rowOf(page, "api").start.disabled, false);
});

// ── the log beside the list ──────────────────────────────────────────────
test("opening a service's log shows its header; the list keeps its place and marks the row", () => {
  const { page } = openWith([running({ id: "1", name: "api", run: runDTO({ started_at: "2026-11-07T14:02:00" }) }), serviceDTO({ id: "2", name: "db" })]);
  assert.equal(page.detailOpen, false);
  assert.equal(page.noDetail, true);
  page.openLog("1");
  assert.equal(page.detailOpen, true);
  assert.deepEqual(page.logIds, ["1"]);
  assert.equal(page.logView.name, "api");
  assert.equal(page.logView.status.word, "running");
  assert.equal(page.logView.runLine, "Run started 14:02 · by you · main checkout @ main");
  assert.equal(rowOf(page, "api").current, true);
  assert.equal(rowOf(page, "db").current, false);
});

test("the log's header follows the service's status live", () => {
  const { page, gateway } = openWith([running({ id: "1", name: "api" })]);
  page.openLog("1");
  gateway.emit({ seq: 2, kind: "service_changed", service: gateway.dtoOf("1", "exited", runDTO({ exit_code: 3, ended_at: "2026-11-07T15:30:00Z" })), running_count: 0 });
  assert.equal(page.logView.status.word, "exited · code 3");
});

test("opening another service's log swaps the viewer; closing it leaves the list alone", () => {
  const { page } = openWith([running({ id: "1", name: "api" }), running({ id: "2", name: "web" })]);
  page.openLog("1");
  page.openLog("2");
  assert.deepEqual(page.logIds, ["2"]);
  page.closeLog();
  assert.deepEqual(page.logIds, []);
  assert.equal(page.noDetail, true);
});

test("a service deleted while its log is open closes the log", () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  page.openLog("1");
  gateway.emit({ seq: 2, kind: "service_deleted", service_id: "1", running_count: 0 });
  assert.equal(page.logId, "");
});

test("a failed service's row opens its log from its reason", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "api", status: "failed", run: runDTO({ error: "boom" }) })]);
  page.openLogRow({ currentTarget: { dataset: { serviceId: "1" } } });
  assert.equal(page.logId, "1");
});

// ── create, edit and delete ──────────────────────────────────────────────
import { toService } from "../../infrastructure/dto.js";

test("New service opens the form empty; Cancel (the form's close) shuts it", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "api" })]);
  assert.equal(page.formOpen, false);
  page.openNew();
  assert.equal(page.formOpen, true);
  assert.equal(page.formService, null);
  page.closeForm();
  assert.equal(page.formOpen, false);
});

test("Edit opens the form on that service, from its menu", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "api" })]);
  page.menuSelect({ currentTarget: { dataset: { serviceId: "1" } }, detail: { action: "edit" } });
  assert.equal(page.formOpen, true);
  assert.equal(page.formService.name, "api");
});

test("a created service joins the list at once, the form closes, and the page says so", () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "web" })]);
  page.openNew();
  page.saved({ detail: { service: toService(serviceDTO({ id: "2", name: "api" })), restartNeeded: false, created: true } });
  assert.deepEqual(rowNames(page), ["api", "web"]);
  assert.equal(page.formOpen, false);
  assert.equal(page.notice, "api was created.");
});

test("an edited service replaces its row; a running one says it needs a restart", () => {
  const { page } = openWith([running({ id: "1", name: "api" })]);
  page.openEdit("1");
  page.saved({ detail: { service: toService(serviceDTO({ id: "1", name: "api", status: "running", run: runDTO() })), restartNeeded: true, created: false } });
  assert.equal(page.notice, "api was saved. Restart it to apply the change.");
  assert.equal(page.formOpen, false);
});

test("Delete asks first; Keep it cancels and nothing is deleted", () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  page.askDelete("1");
  assert.equal(page.del.name, "api");
  assert.equal(page.deleteOpen, true);
  page.cancelDelete();
  assert.equal(page.deleteOpen, false);
  assert.deepEqual(gateway.saves, []);
});

test("confirming deletes the service and takes it off the list", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" }), serviceDTO({ id: "2", name: "db" })]);
  page.askDelete("1");
  await page.confirmDelete();
  assert.deepEqual(gateway.saves, [["delete", "1"]]);
  assert.deepEqual(rowNames(page), ["db"]);
  assert.equal(page.deleteOpen, false);
});

test("Delete is unavailable on a running service", () => {
  const { page } = openWith([running({ id: "1", name: "api" }), serviceDTO({ id: "2", name: "db" })]);
  assert.equal(rowOf(page, "api").canDelete, false);
  assert.equal(rowOf(page, "db").canDelete, true);
  page.askDelete("1");
  assert.equal(page.deleteOpen, false);
});

test("a delete the server refuses (SERVICE_RUNNING) shows in the dialog and keeps the service", async () => {
  const { page, gateway } = openWith([serviceDTO({ id: "1", name: "api" })]);
  gateway.save.remove = async () => { throw new StructuredError("SERVICE_RUNNING", "api is running"); };
  page.askDelete("1");
  await page.confirmDelete();
  assert.equal(page.del.error.code, "SERVICE_RUNNING");
  assert.deepEqual(rowNames(page), ["api"]);
  assert.equal(page.deleteOpen, true);
});

test("deleting the service whose log is open closes the log", async () => {
  const { page } = openWith([serviceDTO({ id: "1", name: "api" })]);
  page.openLog("1");
  page.askDelete("1");
  await page.confirmDelete();
  assert.equal(page.logId, "");
});

// ── "Run in" ─────────────────────────────────────────────────────────────
const memoryPrefs = (initial = {}) => {
  const map = new Map(Object.entries(initial));
  return { map, get: (k) => map.get(k) ?? null, set: (k, v) => (v === null ? map.delete(k) : map.set(k, v)) };
};
const mainWt = worktreeDTO({ key: "main", kind: "main", branch: "main", dir: "/repo" });
const otherWt = worktreeDTO({ key: "session:s2", kind: "session", branch: "agent/other", dir: "/w", session_id: "s2", agent: "go-developer", session_status: "running" });
const withRepo = (o = {}) => serviceDTO({ id: "1", name: "api", repository_id: "r1", ...o });
const openRunIn = async ({ services = [withRepo()], worktrees = [mainWt, otherWt], prefs = memoryPrefs() } = {}) => {
  const gateway = fakeServices({ list: listDTO(services, { seq: 1 }) });
  gateway.worktrees = worktrees;
  const { page } = open({ seed: { project_id: "p1", list: gateway.list }, gateway, prefs });
  page.init();
  await tick();
  return { page, gateway, prefs };
};
const pickRunIn = (page, id, key) => page.runInPicked({ currentTarget: { dataset: { serviceId: id } }, detail: { value: key } });
const reopenRunIn = (page, id) => page.runInOpened({ currentTarget: { dataset: { serviceId: id } } });

test("a command service with a repository offers Run in: the main checkout, then the sessions' worktrees in the server's order", async () => {
  const { page, gateway } = await openRunIn();
  assert.deepEqual(gateway.worktreeReads, ["1"]);
  const runIn = rowOf(page, "api").runIn;
  assert.equal(runIn.show, true);
  assert.deepEqual(runIn.main.map((o) => [o.key, o.label]), [["main", "main"]]);
  assert.deepEqual(runIn.sessions.map((o) => [o.key, o.label, o.description]), [["session:s2", "agent/other", "go-developer · running"]]);
  assert.equal(runIn.pickedKey, "main");
});

test("an image service, or one with no repository, has no picker and reads no worktrees", async () => {
  const image = serviceDTO({ id: "2", name: "db", kind: "image", command: null, image: { ref: "postgres:16", args: [], ports: [] }, repository_id: "r1" });
  const { page, gateway } = await openRunIn({ services: [image, serviceDTO({ id: "3", name: "bare" })] });
  assert.equal(rowOf(page, "db").runIn.show, false);
  assert.equal(rowOf(page, "bare").runIn.show, false);
  assert.deepEqual(gateway.worktreeReads, []);
});

test("the picker is not offered while the service runs: Start is not there either", async () => {
  const { page } = await openRunIn({ services: [running({ id: "1", name: "api", repository_id: "r1" })] });
  assert.equal(rowOf(page, "api").runIn.show, false);
});

test("picking a worktree starts nothing, is remembered under the service's own key, and is announced", async () => {
  const { page, gateway, prefs } = await openRunIn();
  pickRunIn(page, "1", "session:s2");
  assert.deepEqual(gateway.starts, []);
  assert.equal(prefs.get("service-run-worktree:1"), "session:s2");
  assert.equal(prefs.get("app-run-worktree:1"), null);
  assert.equal(rowOf(page, "api").runIn.pickedKey, "session:s2");
  assert.equal(page.notice, "api runs in go-developer, branch agent/other.");
  pickRunIn(page, "1", "main");
  assert.equal(prefs.get("service-run-worktree:1"), null, "the main checkout is the default, so it is forgotten");
});

test("Start sends the pick; Restart sends none", async () => {
  const { page, gateway } = await openRunIn();
  pickRunIn(page, "1", "session:s2");
  await page.start("1");
  assert.deepEqual(gateway.starts, [["1", "session:s2"]]);
  await page.restart("1");
  assert.deepEqual(gateway.actions, ["start:1", "restart:1"]);
  assert.equal(rowOf(page, "api").restart.title, "Repeats the run: the same worktree and the same overrides");
});

test("Start sends nothing while the list loads or when it failed", async () => {
  const gateway = fakeServices({ list: listDTO([withRepo()], { seq: 1 }) });
  gateway.worktreesError = new StructuredError("NETWORK", "down");
  const { page } = open({ seed: { project_id: "p1", list: gateway.list }, gateway, prefs: memoryPrefs() });
  page.init();
  assert.equal(rowOf(page, "api").runIn.loading, "true");
  await page.start("1");
  await tick();
  assert.deepEqual(gateway.starts, [["1", ""]]);
  assert.equal(rowOf(page, "api").runIn.error, "Could not load worktrees.");
  assert.equal(rowOf(page, "api").runIn.loading, null);
});

test("opening the picker reads the list again", async () => {
  const { page, gateway } = await openRunIn();
  await reopenRunIn(page, "1");
  assert.deepEqual(gateway.worktreeReads, ["1", "1"]);
});

test("a remembered pick is restored silently once the list is in", async () => {
  const { page } = await openRunIn({ prefs: memoryPrefs({ "service-run-worktree:1": "session:s2" }) });
  assert.equal(rowOf(page, "api").runIn.pickedKey, "session:s2");
  assert.equal(rowOf(page, "api").runIn.fallback, "");
});

test("a remembered worktree that is gone falls back to the main checkout with a notice, once, and forgets it", async () => {
  const prefs = memoryPrefs({ "service-run-worktree:1": "session:gone" });
  const { page } = await openRunIn({ prefs });
  const runIn = rowOf(page, "api").runIn;
  assert.equal(runIn.pickedKey, "main");
  assert.equal(runIn.fallback, "That worktree no longer exists. Running in the main checkout.");
  assert.equal(prefs.get("service-run-worktree:1"), null);
  page.dismissRunInFallback({ currentTarget: { dataset: { serviceId: "1" } } });
  assert.equal(rowOf(page, "api").runIn.fallback, "");
});

test("a worktree that vanished before Start is refused: the server's message shows, the list is read again, the pick moves", async () => {
  const { page, gateway } = await openRunIn();
  pickRunIn(page, "1", "session:s2");
  gateway.worktrees = [mainWt];
  gateway.answer.start = async () => { throw new StructuredError("WORKTREE_NOT_AVAILABLE", "that worktree is no longer available"); };
  await page.start("1");
  await tick();
  assert.equal(page.refusal.message, "that worktree is no longer available");
  assert.equal(page.refusal.code, "WORKTREE_NOT_AVAILABLE");
  assert.deepEqual(gateway.worktreeReads, ["1", "1"]);
  const runIn = rowOf(page, "api").runIn;
  assert.equal(runIn.pickedKey, "main");
  assert.equal(runIn.fallback, "", "the refusal already says it");
});

test("a service that joins the list later gets its worktrees read", async () => {
  const { page, gateway } = await openRunIn({ services: [] });
  gateway.emit({ seq: 2, kind: "service_changed", service: withRepo({ id: "9", name: "new" }), running_count: 0 });
  await tick();
  assert.deepEqual(gateway.worktreeReads, ["9"]);
  assert.equal(rowOf(page, "new").runIn.show, true);
});

test("the row names where the run runs: the branch and whether it is the main checkout or a session's worktree", () => {
  const main = running({ id: "1", name: "a", repository_id: "r1", run: runDTO({ where: { kind: "main", dir: "/r", branch: "main", session_id: "" } }) });
  const wt = running({ id: "2", name: "b", repository_id: "r1", run: runDTO({ worktree_key: "session:s2", where: { kind: "session", dir: "/w", branch: "agent/other", session_id: "s2" } }) });
  const { page } = openWith([main, wt]);
  assert.equal(rowOf(page, "a").where, "main checkout @ main");
  assert.equal(rowOf(page, "b").where, "session worktree @ agent/other");
});

test("a picker's select follows the pick once its options exist", async () => {
  const { page } = await openRunIn({ prefs: memoryPrefs({ "service-run-worktree:1": "session:s2" }) });
  const select = { value: "main", isConnected: true, dataset: { serviceId: "1" }, options: [{ value: "main" }, { value: "session:s2" }] };
  let after = null;
  const scope = Object.create(page, { $el: { value: select }, $nextTick: { value: (fn) => { after = fn; } } });
  page.syncRunIn.call(scope);
  after();
  assert.equal(select.value, "session:s2");
});

test("the open log's panel knows the run on screen, so its variables follow a new run", () => {
  const { page, gateway } = openWith([running({ id: "1", name: "api" })]);
  page.openLog("1");
  assert.equal(page.logView.runId, "r1");
  gateway.emit({ seq: 2, kind: "service_changed", service: running({ id: "1", name: "api", run: runDTO({ id: "r2" }) }), running_count: 1 });
  assert.equal(page.logView.runId, "r2");
  assert.equal(openWith([serviceDTO({ id: "1", name: "idle" })]).page.groups[0].rows.length, 1);
});

test("a row whose run has agent overrides says so; a run without them has no mark", () => {
  const withOverrides = running({ id: "1", name: "api", run: runDTO({ overrides: [{ name: "A", by: "go-developer · session abcdef12" }, { name: "B", by: "go-developer · session abcdef12" }] }) });
  const { page } = openWith([withOverrides, running({ id: "2", name: "web" })]);
  assert.equal(rowOf(page, "api").overrides, "2 overrides by go-developer");
  assert.equal(rowOf(page, "web").overrides, "");
});

// ── "Run in": a branch ───────────────────────────────────────────────────
const branches = [
  branchDTO({ name: "feature/x", sha: "abc1234", subject: "Add the thing", committed_at: "2026-11-07T14:00:00Z" }),
  branchDTO({ name: "dev", remote: true, sha: "def5678", subject: "Work in progress", committed_at: "2026-11-07T10:00:00Z" }),
];
const openBranchRunIn = async (o = {}) => {
  const world = await openRunIn(o);
  world.gateway.branches = branches;
  return world;
};
const pickBranch = (page, id, key) => page.branchPicked({ currentTarget: { dataset: { serviceId: id } }, detail: { value: key } });

test("the picker ends with a Branch… entry, which opens the search without changing the pick or starting anything", async () => {
  const { page, gateway } = await openBranchRunIn();
  const entry = rowOf(page, "api").runIn.branchEntry;
  assert.deepEqual([entry.key, entry.label], ["branch:", "Branch…"]);
  assert.equal(rowOf(page, "api").runIn.searchOpen, false);
  pickRunIn(page, "1", "branch:");
  assert.equal(rowOf(page, "api").runIn.searchOpen, true);
  assert.equal(rowOf(page, "api").runIn.pickedKey, "main");
  assert.deepEqual(gateway.starts, []);
});

test("the search asks the server with the typed text and lists local branches before remote ones", async () => {
  const { page, gateway } = await openBranchRunIn();
  pickRunIn(page, "1", "branch:");
  const all = await page.searchBranches("", new AbortController().signal);
  assert.deepEqual(all.map((o) => [o.value, o.group]), [["branch:feature/x", "Local branches"], ["branch:origin/dev", "Remote branches · fetched first"]]);
  assert.equal(all[0].meta, "2h ago");
  const narrowed = await page.searchBranches("DEV", new AbortController().signal);
  assert.deepEqual(narrowed.map((o) => o.value), ["branch:origin/dev"]);
  assert.deepEqual(gateway.branchQueries, ["", "DEV"]);
});

test("a server that says there are more than 200 adds the line that says so", async () => {
  const { page, gateway } = await openBranchRunIn();
  gateway.branchesTruncated = true;
  pickRunIn(page, "1", "branch:");
  const options = await page.searchBranches("", new AbortController().signal);
  assert.deepEqual([options.at(-1).label, options.at(-1).disabled], ["Showing the first 200, type to narrow", true]);
});

test("a branch list that cannot be read is the combobox's error: the source rejects", async () => {
  const { page, gateway } = await openBranchRunIn();
  gateway.branchesError = new StructuredError("NETWORK", "down");
  pickRunIn(page, "1", "branch:");
  const err = await page.searchBranches("", new AbortController().signal).catch((e) => e);
  assert.equal(err.code, "NETWORK");
});

test("picking a branch sets the pick, closes the search, is remembered and announced, and Start sends it", async () => {
  const { page, gateway, prefs } = await openBranchRunIn();
  pickRunIn(page, "1", "branch:");
  pickBranch(page, "1", "branch:origin/dev");
  const runIn = rowOf(page, "api").runIn;
  assert.deepEqual([runIn.searchOpen, runIn.pickedKey, runIn.branch.label, runIn.hasBranch], [false, "branch:origin/dev", "origin/dev", true]);
  assert.equal(prefs.get("service-run-worktree:1"), "branch:origin/dev");
  assert.equal(page.notice, "api runs branch origin/dev.");
  assert.deepEqual(gateway.starts, []);
  await page.start("1");
  assert.deepEqual(gateway.starts, [["1", "branch:origin/dev"]]);
});

test("Cancel closes the search and keeps the previous pick", async () => {
  const { page } = await openBranchRunIn();
  pickRunIn(page, "1", "session:s2");
  pickRunIn(page, "1", "branch:");
  page.cancelBranchSearch({ currentTarget: { closest: () => null } });
  assert.deepEqual([rowOf(page, "api").runIn.searchOpen, rowOf(page, "api").runIn.pickedKey], [false, "session:s2"]);
});

test("the search is shown for one service at a time", async () => {
  const { page } = await openBranchRunIn({ services: [withRepo(), withRepo({ id: "2", name: "web" })] });
  pickRunIn(page, "1", "branch:");
  pickRunIn(page, "2", "branch:");
  assert.deepEqual([rowOf(page, "api").runIn.searchOpen, rowOf(page, "web").runIn.searchOpen], [false, true]);
});

test("a branch pick is restored after a reload from its key alone, and a re-read of the list keeps it", async () => {
  const { page } = await openBranchRunIn({ prefs: memoryPrefs({ "service-run-worktree:1": "branch:feature/x" }) });
  const runIn = rowOf(page, "api").runIn;
  assert.deepEqual([runIn.pickedKey, runIn.branch.label, runIn.fallback], ["branch:feature/x", "feature/x", ""]);
  await reopenRunIn(page, "1");
  assert.equal(rowOf(page, "api").runIn.pickedKey, "branch:feature/x");
});

test("a branch that no longer exists is refused: the server's message shows, the pick is forgotten and goes back to the main checkout", async () => {
  const { page, gateway, prefs } = await openBranchRunIn();
  pickBranch(page, "1", "branch:feature/gone");
  gateway.answer.start = async () => { throw new StructuredError("WORKTREE_NOT_AVAILABLE", 'branch "feature/gone" does not exist in api'); };
  await page.start("1");
  await tick();
  assert.equal(page.refusal.message, 'branch "feature/gone" does not exist in api');
  assert.equal(page.refusal.code, "WORKTREE_NOT_AVAILABLE");
  const runIn = rowOf(page, "api").runIn;
  assert.deepEqual([runIn.pickedKey, runIn.hasBranch, runIn.fallback], ["main", false, ""]);
  assert.equal(prefs.get("service-run-worktree:1"), null);
});

test("Restart on a branch run says it runs the branch again at its latest commit", () => {
  const onBranch = running({ id: "1", name: "api", repository_id: "r1", run: runDTO({ worktree_key: "branch:feature/x", where: { kind: "branch", dir: "/s", branch: "feature/x", sha: "abc1234", remote: false, session_id: "" } }) });
  const { page } = openWith([onBranch]);
  assert.equal(rowOf(page, "api").restart.title, "Runs the same branch again at its latest commit");
});

test("Start while the branch search is open closes it and runs the pick", async () => {
  const { page, gateway } = await openBranchRunIn();
  pickRunIn(page, "1", "branch:");
  await page.start("1");
  assert.equal(rowOf(page, "api").runIn.searchOpen, false);
  assert.deepEqual(gateway.starts, [["1", "main"]]);
});
