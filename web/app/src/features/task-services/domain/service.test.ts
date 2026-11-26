import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { exited, service, TASK } from "../testing/fixtures";
import { belongsToTask, groupServices, isFalha, portLinks, statusView, timeFact } from "./service";

describe("serviço da task", () => {
  test("a service belongs to the task that created it or started its run, and to no other", () => {
    assert.equal(belongsToTask(service(), TASK), true);
    assert.equal(belongsToTask(service({ createdBy: { kind: "person", taskId: "", agent: "" } }), TASK), true, "run started by the task");
    const person = { kind: "person" as const, taskId: "", agent: "" };
    assert.equal(belongsToTask(service({ createdBy: { ...person, taskId: TASK }, run: null }), TASK), true, "created by the task");
    assert.equal(belongsToTask(service({ createdBy: { ...person, taskId: "other" }, run: { ...service().run!, startedBy: { ...person, taskId: "other" } } }), TASK), false);
    assert.equal(belongsToTask(service({ createdBy: person, run: null }), TASK), false);
  });
});

describe("falha de serviço and status words", () => {
  test("failed and exited with a non-zero code are falhas; exited 0 is stopped", () => {
    assert.equal(isFalha(service({ status: "failed" })), true);
    assert.equal(isFalha(exited(2)), true);
    assert.equal(isFalha(exited(0)), false);
    assert.equal(isFalha(service({ status: "stopped" })), false);
  });

  test("each state reads as the UX study words it", () => {
    assert.deepEqual(statusView(service({ status: "stopped", run: null })), { dot: "created", word: "created · never started" });
    assert.deepEqual(statusView(service()), { dot: "running", word: "running" });
    assert.deepEqual(statusView(service({ status: "starting" })), { dot: "starting", word: "starting" });
    assert.deepEqual(statusView(exited(0)), { dot: "stopped", word: "stopped · exit code 0" });
    assert.deepEqual(statusView(service({ status: "stopped" })), { dot: "stopped", word: "stopped by you" });
    assert.deepEqual(statusView(exited(3)), { dot: "failed", word: "failed · exit code 3" });
    assert.deepEqual(statusView(service({ status: "failed" })), { dot: "failed", word: "failed" });
  });
});

describe("groups", () => {
  test("Needs attention, Running, Not running; each by name; empty groups left out", () => {
    const groups = groupServices([
      service({ id: "1", name: "web", status: "stopped" }),
      service({ id: "2", name: "db" }),
      exited(1, { id: "3", name: "worker" }),
      service({ id: "4", name: "api" }),
    ]);
    assert.deepEqual(groups.map((g) => [g.label, g.services.map((s) => s.name)]), [["Needs attention", ["worker"]], ["Running", ["api", "db"]], ["Not running", ["web"]]]);
    assert.deepEqual(groupServices([]), []);
  });
});

describe("port links", () => {
  test("a command's ports link to localhost", () => {
    assert.deepEqual(portLinks(service()), [{ text: ":8080", href: "http://localhost:8080", name: "Open localhost:8080 (api)" }]);
  });

  test("an image links its host port, shows a different container port, and a UDP port is text", () => {
    const links = portLinks(service({ command: null, image: { ref: "pg", ports: [{ host: 5433, container: 5432, protocol: "tcp" }, { host: 53, container: 53, protocol: "udp" }, { host: 80, container: 80, protocol: "tcp" }] } }));
    assert.deepEqual(links.map((l) => [l.text, l.href]), [[":5433 → 5432", "http://localhost:5433"], [":53/udp", ""], [":80", "http://localhost:80"]]);
  });
});

describe("time fact", () => {
  const now = Date.parse("2026-11-25T10:10:00Z");
  test("running shows how long it is up; stopped shows when it ended; never run says so", () => {
    assert.equal(timeFact(service(), now), "up 10m");
    assert.equal(timeFact(exited(0), now), "ended 5m ago");
    assert.equal(timeFact(service({ status: "stopped", run: null }), now), "never run");
  });
});
