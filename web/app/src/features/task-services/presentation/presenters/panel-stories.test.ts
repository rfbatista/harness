// The spec's user stories 3, 5, 11, 13, 14 and 17 at the presenter seam, beyond panel-presenter.test.ts.
import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { exited, request, service, TASK } from "../../testing/fixtures";
import { memoryServicesSource } from "../../testing/memory-services-source";
import { panel, rows, settle } from "../../testing/harness";

const elsewhere = { kind: "session" as const, taskId: "other", agent: "x" };
const movedAway = (over: Parameters<typeof service>[0] = {}) =>
  service({ createdBy: elsewhere, run: { ...service().run!, startedBy: elsewhere }, ...over });
const toastText = (p: { viewModel: { toast: { text: string } | null } }) => p.viewModel.toast?.text;
const changed =(seq: number, s: ReturnType<typeof service>) => ({ kind: "service_changed" as const, seq, service: s });

describe("story 3 and 11: the task's list follows who owns the service", () => {
  test("a service another task's run took over leaves the panel and the selo", async () => {
    const source = memoryServicesSource({ services: [exited(1)], requests: [] });
    const { presenter } = await panel(source);
    assert.equal(presenter.viewModel.selo.value, 1);
    source.emit(changed(2, movedAway({ status: "running" })));
    assert.equal(rows(presenter).length, 0);
    assert.equal(presenter.viewModel.empty, true);
    assert.equal(presenter.viewModel.selo.value, 0);
  });

  test("a restart from this task's page keeps the service here (run started by this task)", async () => {
    const source = memoryServicesSource({ services: [movedAway({ createdBy: { kind: "person", taskId: TASK, agent: "" }, status: "stopped", run: null })] });
    const { presenter } = await panel(source);
    source.emit(changed(2, movedAway({ createdBy: elsewhere, status: "running", run: { ...service().run!, startedBy: { kind: "person", taskId: TASK, agent: "" } } })));
    assert.equal(rows(presenter).length, 1);
  });

  test("another task's service failing later raises no toast and no selo", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    source.emit(changed(2, movedAway({ id: "z", name: "foreign", status: "failed" })));
    assert.equal(presenter.viewModel.toast, null);
    assert.equal(presenter.viewModel.selo.value, 0);
  });

  test("a re-list drops a service that left the task while the feed was away", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "a" }), service({ id: "b", name: "b" })], seq: 1 });
    const { presenter } = await panel(source);
    source.services = [service({ id: "a", name: "a" }), movedAway({ id: "b", name: "b" })];
    source.seq = 7;
    source.status("resynced");
    await settle();
    assert.deepEqual(rows(presenter).map((r) => r.name), ["a"]);
  });
});

describe("story 5: exit code decides falha or parado", () => {
  test("exited 0 sits outside attention and counts nowhere; non-zero is attention and in the selo", async () => {
    const source = memoryServicesSource({ services: [exited(0, { id: "ok", name: "ok" }), exited(2, { id: "bad", name: "bad" })] });
    const { presenter } = await panel(source);
    const byName = Object.fromEntries(rows(presenter).map((r) => [r.name, r]));
    assert.notEqual(byName.ok!.group, "attention");
    assert.equal(byName.ok!.dot, "stopped");
    assert.equal(byName.bad!.group, "attention");
    assert.equal(byName.bad!.dot, "failed");
    assert.equal(presenter.viewModel.selo.value, 1);
  });

  test("a running service that exits 0 raises no toast; one that exits 1 does", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "a" }), service({ id: "b", name: "b" })] });
    const { presenter } = await panel(source);
    source.emit(changed(2, exited(0, { id: "a", name: "a" })));
    assert.equal(presenter.viewModel.toast, null);
    source.emit(changed(3, exited(1, { id: "b", name: "b" })));
    assert.equal(toastText(presenter), "b failed · exit code 1");
  });

  test("a service that fails again after running once more raises a new toast", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter, clock } = await panel(source);
    source.emit(changed(2, exited(1)));
    presenter.dismissToast();
    clock.advance(5_000);
    source.emit(changed(3, service()));
    assert.equal(presenter.viewModel.selo.value, 0);
    source.emit(changed(4, service({ status: "failed" })));
    assert.equal(toastText(presenter)?.startsWith("api failed"), true);
  });
});

describe("story 13: the selo counts falhas plus this task's pending requests only", () => {
  test("it drops as each falha and each request resolves, and a deleted failing service leaves it", async () => {
    const source = memoryServicesSource({
      services: [exited(1, { id: "a", name: "a" }), exited(2, { id: "b", name: "b" })],
      requests: [request({ id: "r1" }), request({ id: "r2" })],
    });
    const { presenter } = await panel(source);
    assert.equal(presenter.viewModel.selo.value, 4);
    assert.equal(presenter.viewModel.selo.tone, "danger");
    source.emit({ kind: "service_request", seq: 2, request: request({ id: "r1", state: "denied" }) });
    assert.equal(presenter.viewModel.selo.value, 3);
    source.emit({ kind: "service_deleted", seq: 3, serviceId: "a" });
    assert.equal(presenter.viewModel.selo.value, 2);
    source.emit(changed(4, service({ id: "b", name: "b" })));
    assert.equal(presenter.viewModel.selo.value, 1);
    assert.equal(presenter.viewModel.selo.tone, "attention");
    source.emit({ kind: "service_request", seq: 5, request: request({ id: "r2", state: "approved" }) });
    assert.equal(presenter.viewModel.selo.value, 0);
    assert.equal(presenter.viewModel.selo.label, "");
  });

  test("another task's requests never count, joined or loaded", async () => {
    const source = memoryServicesSource({ services: [service()], requests: [request({ id: "x", taskId: "other" })] });
    const { presenter } = await panel(source);
    source.emit({ kind: "service_request", seq: 2, request: request({ id: "y", taskId: "other" }) });
    assert.equal(presenter.viewModel.selo.value, 0);
  });

  test("a re-list replaces the pending requests with the server's current ones", async () => {
    const source = memoryServicesSource({ services: [service()], requests: [request({ id: "r1" })] });
    const { presenter } = await panel(source);
    source.pending = [request({ id: "r2" })];
    source.status("resynced");
    await settle();
    assert.deepEqual(presenter.viewModel.requests.map((r) => r.id), ["r2"]);
    assert.equal(presenter.viewModel.selo.value, 1);
  });
});

describe("story 17: resync and seq gaps", () => {
  test("a gap re-lists, applies the list, and does not lose or double the event after the gap", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "a" })], seq: 1 });
    const { presenter } = await panel(source);
    source.services = [service({ id: "a", name: "a" }), service({ id: "b", name: "b" })];
    source.seq = 3;
    source.emit(changed(4, service({ id: "c", name: "c" })));
    await settle();
    assert.deepEqual(rows(presenter).map((r) => r.name).sort(), ["a", "b", "c"]);
    source.emit(changed(4, service({ id: "c", name: "c", status: "stopped" })));
    assert.equal(rows(presenter).find((r) => r.name === "c")!.dot, "running", "a replayed seq is ignored");
  });

  test("events past the gap that the list still does not reach are not applied", async () => {
    const source = memoryServicesSource({ services: [service()], seq: 1 });
    const { presenter } = await panel(source);
    source.seq = 3;
    source.emit(changed(9, service({ id: "q", name: "far" })));
    await settle();
    assert.equal(source.listCalls, 2, "one re-list, no loop");
    assert.deepEqual(rows(presenter).map((r) => r.name), ["api"]);
  });

  test("the live flag follows the stream status", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    source.status("live");
    assert.equal(presenter.viewModel.live, true);
    source.status("paused");
    assert.equal(presenter.viewModel.live, false);
  });

  test("a failed list shows an error and keeps the last state; retry recovers", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    const list = source.list;
    source.list = async () => {
      throw new Error("offline");
    };
    source.status("resynced");
    await settle();
    assert.equal(presenter.viewModel.error, "offline");
    assert.equal(rows(presenter).length, 1);
    source.list = list;
    presenter.retry();
    await settle();
    assert.equal(presenter.viewModel.error, "");
  });
});

describe("story 14: the toast is for failures seen on the feed", () => {
  test("a failure that arrives in a re-list after the page was open raises none, a later transition does", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "a" }), service({ id: "b", name: "b" })], seq: 1 });
    const { presenter } = await panel(source);
    source.services = [exited(1, { id: "a", name: "a" }), service({ id: "b", name: "b" })];
    source.seq = 5;
    source.status("resynced");
    await settle();
    assert.equal(presenter.viewModel.toast, null);
    source.emit(changed(6, exited(1, { id: "b", name: "b" })));
    assert.equal(toastText(presenter), "b failed · exit code 1");
  });
});
