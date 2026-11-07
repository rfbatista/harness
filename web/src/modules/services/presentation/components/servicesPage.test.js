import { StructuredError } from "../../../../shared/domain/errors.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { fakeServices, listDTO, pageEl, runDTO, serviceDTO } from "../../testing/fakeGateway.js";
import { servicesPage } from "./servicesPage.js";

file("services/presentation/components/servicesPage");

const NOW = "2026-11-07T16:00:00Z";
const running = (o = {}) => serviceDTO({ status: "running", run: runDTO({ started_at: "2026-11-07T14:00:00Z" }), ...o });

function open({ seed, gateway = fakeServices() } = {}) {
  const timers = { intervals: [], setInterval: (fn) => (timers.intervals.push(fn), timers.intervals.length), clearInterval() {} };
  const el = pageEl(seed ?? { project_id: "p1", list: listDTO([running({ id: "1", name: "web" }), serviceDTO({ id: "2", name: "db" })], { seq: 4 }) });
  const { instance } = mount(servicesPage({ gateway, clock: fixedClock(NOW), timers }), { el });
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
