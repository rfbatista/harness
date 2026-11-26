import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { exited, request, service, TASK } from "../../testing/fixtures";
import { memoryServicesSource, refusal } from "../../testing/memory-services-source";
import { memoryStorage, panel, rows, settle } from "../../testing/harness";
import { panelKey } from "../../infrastructure/storage-panel-preferences";
import { isToggleKey, seloOf } from "./panel-presenter";

const other = { createdBy: { kind: "session" as const, taskId: "other", agent: "x" }, run: { ...service().run!, startedBy: { kind: "session" as const, taskId: "other", agent: "x" } } };
const changed = (seq: number, s: ReturnType<typeof service>) => ({ kind: "service_changed" as const, seq, service: s });

describe("the task filter over the list and the feed", () => {
  test("only the task's services show, from the first list and from feed events", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "api" }), service({ id: "b", name: "other", ...other })] });
    const { presenter } = await panel(source);
    assert.deepEqual(rows(presenter).map((r) => r.name), ["api"]);
    source.emit(changed(2, service({ id: "c", name: "db" })));
    source.emit(changed(3, service({ id: "d", name: "foreign", ...other })));
    assert.deepEqual(rows(presenter).map((r) => r.name), ["api", "db"]);
  });

  test("service_deleted removes the row", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    source.emit({ kind: "service_deleted", seq: 2, serviceId: "svc-1" });
    assert.equal(rows(presenter).length, 0);
    assert.equal(presenter.viewModel.empty, true);
  });

  test("an event at or below the list's seq is ignored", async () => {
    const source = memoryServicesSource({ services: [service({ status: "stopped" })], seq: 5 });
    const { presenter } = await panel(source);
    source.emit(changed(5, service({ status: "running" })));
    assert.equal(rows(presenter)[0]!.dot, "stopped");
  });

  test("a seq gap or a resynced feed re-lists", async () => {
    const source = memoryServicesSource({ services: [service()], seq: 1 });
    const { presenter } = await panel(source);
    assert.equal(source.listCalls, 1);
    source.seq = 4;
    source.emit(changed(4, service({ id: "z", name: "late" })));
    await settle();
    assert.equal(source.listCalls, 2);
    source.status("resynced");
    await settle();
    assert.equal(source.listCalls, 3);
    assert.equal(presenter.viewModel.live, false);
  });

  test("events that arrive while the first list loads are applied after it", async () => {
    const source = memoryServicesSource({ services: [service({ status: "stopped", run: null })], seq: 1 });
    const { presenter } = await (async () => {
      const early = panel(source);
      source.emit(changed(2, service({ status: "running" })));
      return early;
    })();
    await settle();
    assert.equal(rows(presenter)[0]!.dot, "running");
  });

  test("rows carry the status word, what runs, ports and the first error line of a falha", async () => {
    const source = memoryServicesSource({ services: [exited(2, { run: { ...exited(2).run!, error: "boom\nstack" } })] });
    const { presenter } = await panel(source);
    const row = rows(presenter)[0]!;
    assert.equal(row.group, "attention");
    assert.equal(row.word, "failed · exit code 2");
    assert.equal(row.what, "go run ./cmd/api");
    assert.equal(row.error, "boom");
    assert.equal(row.ports[0]!.href, "http://localhost:8080");
  });
});

describe("selo", () => {
  test("counts falhas plus pending requests; danger with a falha, attention with only requests, hidden at 0", () => {
    assert.deepEqual(seloOf(0, 0), { value: 0, tone: "", label: "" });
    assert.deepEqual(seloOf(1, 1), { value: 2, tone: "danger", label: "1 failed service, 1 request waiting on you" });
    assert.deepEqual(seloOf(0, 2), { value: 2, tone: "attention", label: "2 requests waiting on you" });
    assert.equal(seloOf(2, 0).label, "2 failed services");
  });

  test("follows current state: it drops as the falha is cleared and as the request is resolved", async () => {
    const source = memoryServicesSource({ services: [exited(1)], requests: [request()] });
    const { presenter } = await panel(source);
    assert.equal(presenter.viewModel.selo.value, 2);
    source.emit(changed(2, service({ status: "running" })));
    assert.equal(presenter.viewModel.selo.value, 1);
    source.emit({ kind: "service_request", seq: 3, request: request({ state: "approved" }) });
    assert.equal(presenter.viewModel.selo.value, 0);
  });
});

describe("collapse and the person's choice", () => {
  test("open by default when the task has a service; collapsed when it has none", async () => {
    assert.equal((await panel(memoryServicesSource({ services: [service()] }))).presenter.viewModel.open, true);
    assert.equal((await panel(memoryServicesSource())).presenter.viewModel.open, false);
  });

  test("collapsed by default with a session pane open", async () => {
    const { presenter } = await panel(memoryServicesSource({ services: [service()] }));
    presenter.setSessionPane(true);
    assert.equal(presenter.viewModel.open, false);
  });

  test("the stored choice wins over the default, and a toggle is stored", async () => {
    const storage = memoryStorage({ [panelKey(TASK)]: "collapsed" });
    const { presenter } = await panel(memoryServicesSource({ services: [service()] }), { storage });
    assert.equal(presenter.viewModel.open, false);
    presenter.toggle();
    assert.equal(presenter.viewModel.open, true);
    assert.equal(storage.data.get(panelKey(TASK)), "open");
  });

  test("a storage that throws falls back to the defaults", async () => {
    const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
    const { presenter } = await panel(memoryServicesSource({ services: [service()] }), { storage: broken as never });
    assert.equal(presenter.viewModel.open, true);
    presenter.toggle();
    assert.equal(presenter.viewModel.open, false);
  });

  test("the panel does not open by itself when a service arrives; the handle signals", async () => {
    const source = memoryServicesSource();
    const { presenter } = await panel(source);
    const before = presenter.viewModel.signal;
    source.emit(changed(2, service()));
    assert.equal(presenter.viewModel.open, false);
    assert.equal(presenter.viewModel.signal, before + 1);
    assert.deepEqual(presenter.viewModel.dots, ["running"]);
  });

  test("S toggles unless the person is typing or holds a modifier", () => {
    assert.equal(isToggleKey({ key: "s", target: { tagName: "DIV" } }), true);
    assert.equal(isToggleKey({ key: "S" }), true);
    assert.equal(isToggleKey({ key: "s", ctrlKey: true }), false);
    assert.equal(isToggleKey({ key: "s", metaKey: true }), false);
    assert.equal(isToggleKey({ key: "s", target: { tagName: "INPUT" } }), false);
    assert.equal(isToggleKey({ key: "s", target: { tagName: "DIV", isContentEditable: true } }), false);
    assert.equal(isToggleKey({ key: "c" }), false);
  });

  test("the empty state shows for a task with nothing", async () => {
    const { presenter } = await panel(memoryServicesSource());
    assert.equal(presenter.viewModel.empty, true);
    assert.deepEqual(presenter.viewModel.dots, []);
  });
});

describe("narrow layouts", () => {
  test("overlay and sheet start collapsed whatever the stored choice; opening is not stored; Escape collapses the overlay", async () => {
    const storage = memoryStorage({ [panelKey(TASK)]: "open" });
    const { presenter } = await panel(memoryServicesSource({ services: [service()] }), { storage });
    presenter.setMode("overlay");
    assert.equal(presenter.viewModel.open, false);
    presenter.toggle();
    assert.equal(presenter.viewModel.open, true);
    assert.equal(storage.data.get(panelKey(TASK)), "open");
    assert.equal(presenter.key("Escape"), true);
    assert.equal(presenter.viewModel.open, false);
    presenter.setMode("sheet");
    assert.equal(presenter.viewModel.mode, "sheet");
    assert.equal(presenter.viewModel.open, false);
    presenter.setMode("docked");
    assert.equal(presenter.viewModel.open, true);
  });
});

describe("toast on falha", () => {
  test("a service moving into falha raises one toast with its code and the task", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    source.emit(changed(2, exited(3)));
    assert.equal(presenter.viewModel.toast?.text, "api failed · exit code 3");
    assert.equal(presenter.viewModel.toast?.context, "Fix the build · started by claude");
  });

  test("starts, stops and clean exits raise none", async () => {
    const source = memoryServicesSource({ services: [service({ status: "stopped", run: null })] });
    const { presenter } = await panel(source);
    source.emit(changed(2, service({ status: "starting" })));
    source.emit(changed(3, service()));
    source.emit(changed(4, exited(0)));
    source.emit(changed(5, service({ status: "stopped" })));
    assert.equal(presenter.viewModel.toast, null);
  });

  test("a failure present in the first list raises none, nor does one that stays a failure", async () => {
    const source = memoryServicesSource({ services: [exited(1)] });
    const { presenter } = await panel(source);
    assert.equal(presenter.viewModel.toast, null);
    source.emit(changed(2, exited(1, { name: "api" })));
    assert.equal(presenter.viewModel.toast, null);
  });

  test("a failure first seen through a resync raises none", async () => {
    const source = memoryServicesSource({ services: [service()], seq: 1 });
    const { presenter } = await panel(source);
    source.services = [exited(1)];
    source.seq = 9;
    source.status("resynced");
    await settle();
    assert.equal(rows(presenter)[0]!.group, "attention");
    assert.equal(presenter.viewModel.toast, null);
  });

  test("two failures within 2s merge; a later one is a toast of its own", async () => {
    const source = memoryServicesSource({ services: [service({ id: "a", name: "a" }), service({ id: "b", name: "b" }), service({ id: "c", name: "c" })] });
    const { presenter, clock } = await panel(source);
    source.emit(changed(2, exited(1, { id: "a", name: "a" })));
    clock.advance(1_500);
    source.emit(changed(3, exited(1, { id: "b", name: "b" })));
    assert.equal(presenter.viewModel.toast?.text, "2 services failed");
    clock.advance(2_500);
    source.emit(changed(4, exited(1, { id: "c", name: "c" })));
    assert.equal(presenter.viewModel.toast?.text, "c failed · exit code 1");
  });

  test("it stays until dismissed, or 10s after the pointer and focus leave it", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter, clock } = await panel(source);
    source.emit(changed(2, exited(1)));
    clock.advance(60_000);
    assert.notEqual(presenter.viewModel.toast, null);
    presenter.toastEngaged(true);
    presenter.toastEngaged(false);
    clock.advance(9_000);
    assert.notEqual(presenter.viewModel.toast, null);
    presenter.toastEngaged(true);
    clock.advance(30_000);
    assert.notEqual(presenter.viewModel.toast, null);
    presenter.toastEngaged(false);
    clock.advance(10_000);
    assert.equal(presenter.viewModel.toast, null);
  });

  test("Show in panel opens a collapsed panel and points at the row; Open log opens the log", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source, { storage: memoryStorage({ [panelKey(TASK)]: "collapsed" }) });
    source.emit(changed(2, exited(1)));
    presenter.showInPanel();
    assert.equal(presenter.viewModel.open, true);
    assert.equal(presenter.viewModel.focusId, "svc-1");
    assert.equal(rows(presenter)[0]!.ringed, true);
    assert.equal(presenter.viewModel.toast, null);
    source.emit(changed(3, service()));
    source.emit(changed(4, exited(1)));
    presenter.openLogFromToast();
    assert.equal(presenter.viewModel.logOpen, true);
  });
});

describe("pedidos de serviço", () => {
  test("only this task's pending requests show; a request on the feed joins, an answered one leaves", async () => {
    const source = memoryServicesSource({ services: [service()], requests: [request(), request({ id: "other", taskId: "other" })] });
    const { presenter } = await panel(source);
    assert.deepEqual(presenter.viewModel.requests.map((r) => r.id), ["req-1"]);
    source.emit({ kind: "service_request", seq: 2, request: request({ id: "req-2" }) });
    source.emit({ kind: "service_request", seq: 3, request: request({ id: "req-9", taskId: "other" }) });
    assert.deepEqual(presenter.viewModel.requests.map((r) => r.id), ["req-1", "req-2"]);
    source.emit({ kind: "service_request", seq: 4, request: request({ state: "denied" }) });
    assert.deepEqual(presenter.viewModel.requests.map((r) => r.id), ["req-2"]);
  });

  test("approve posts the decision, shows the outcome for 3s, then the card is gone; the selo drops at once", async () => {
    const source = memoryServicesSource({ services: [service()], requests: [request()] });
    const { presenter, clock } = await panel(source);
    assert.equal(rows(presenter)[0]!.requestBadge, 1);
    const done = presenter.answer("req-1", "approve");
    assert.equal(presenter.viewModel.requests[0]!.busy, true);
    await done;
    assert.deepEqual(source.answers, [{ requestId: "req-1", decision: "approve" }]);
    assert.equal(presenter.viewModel.requests[0]!.outcome, "Approved · api");
    assert.equal(presenter.viewModel.selo.value, 0);
    clock.advance(3_000);
    assert.equal(presenter.viewModel.requests.length, 0);
  });

  test("a refusal shows its code and the card stays to try again", async () => {
    const source = memoryServicesSource({ services: [service()], requests: [request()] });
    source.failNextAnswer = refusal("REQUEST_NOT_PENDING", "already answered");
    const { presenter } = await panel(source);
    await presenter.answer("req-1", "deny");
    assert.deepEqual(presenter.viewModel.requests[0]!.refusal, { code: "REQUEST_NOT_PENDING", message: "already answered" });
    assert.equal(presenter.viewModel.requests[0]!.busy, false);
    await presenter.answer("req-1", "deny");
    assert.equal(presenter.viewModel.requests[0]!.refusal, null);
  });
});

describe("start, stop and restart", () => {
  test("stop and restart ask first; Cancel and Escape leave no request", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.request("svc-1", "stop");
    assert.equal(rows(presenter)[0]!.confirm, "stop");
    assert.match(rows(presenter)[0]!.confirmText, /^Stop api\? The run ends now; claude may be using it\. Logs stay\.$/);
    presenter.cancel("svc-1");
    presenter.request("svc-1", "restart");
    assert.equal(presenter.key("Escape"), true);
    assert.equal(rows(presenter)[0]!.confirm, null);
    assert.equal(source.acts.length, 0);
  });

  test("confirming posts the verb with the service and the task, and the row reads stopping…", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.request("svc-1", "stop");
    presenter.confirm("svc-1");
    assert.deepEqual(source.acts, [{ verb: "stop", serviceId: "svc-1", taskId: TASK }]);
    const row = rows(presenter)[0]!;
    assert.deepEqual([row.word, row.dot, row.busy], ["stopping…", "pending", true]);
  });

  test("the feed settles the row, even when it arrives before the POST answers; the answer alone does not", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.request("svc-1", "restart");
    presenter.confirm("svc-1");
    assert.equal(rows(presenter)[0]!.word, "restarting…");
    source.emit(changed(2, service({ status: "starting" })));
    assert.equal(rows(presenter)[0]!.busy, false, "settled by the echo");
    source.finishAct(service({ status: "starting" }));
    await settle();
    assert.equal(rows(presenter)[0]!.word, "starting");

    presenter.request("svc-1", "stop");
    presenter.confirm("svc-1");
    source.finishAct(service({ status: "stopped" }));
    await settle();
    assert.equal(rows(presenter)[0]!.busy, true, "the POST's answer does not settle it");
    source.emit(changed(3, service({ status: "stopped" })));
    assert.equal(rows(presenter)[0]!.busy, false);
  });

  test("a row whose feed event never comes is settled from the answer after a while", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter, clock } = await panel(source);
    presenter.request("svc-1", "stop");
    presenter.confirm("svc-1");
    source.finishAct(service({ status: "stopped" }));
    await settle();
    clock.advance(5_000);
    assert.deepEqual([rows(presenter)[0]!.busy, rows(presenter)[0]!.word], [false, "stopped by you"]);
  });

  test("other actions are disabled while pending: a second request is ignored", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.request("svc-1", "restart");
    presenter.confirm("svc-1");
    presenter.request("svc-1", "stop");
    assert.equal(rows(presenter)[0]!.confirm, null);
    assert.equal(source.acts.length, 1);
  });

  test("a refusal shows its code; Try again clears it", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.request("svc-1", "restart");
    presenter.confirm("svc-1");
    source.finishAct(refusal("SERVICE_BUSY", "busy"));
    await settle();
    const row = rows(presenter)[0]!;
    assert.deepEqual([row.busy, row.refusal], [false, { code: "SERVICE_BUSY", message: "busy" }]);
    presenter.dismissRefusal("svc-1");
    assert.equal(rows(presenter)[0]!.refusal, null);
  });

  test("a never-run service offers Start, which goes straight through; the others offer Restart, and Stop only while active", async () => {
    const source = memoryServicesSource({ services: [service({ id: "n", name: "n", status: "stopped", run: null }), service({ id: "r", name: "r" }), exited(0, { id: "s", name: "s" })] });
    const { presenter } = await panel(source);
    const by = Object.fromEntries(rows(presenter).map((r) => [r.name, r]));
    assert.deepEqual([by.n!.canStart, by.n!.canRestart, by.n!.canStop], [true, false, false]);
    assert.deepEqual([by.r!.canStart, by.r!.canRestart, by.r!.canStop], [false, true, true]);
    assert.deepEqual([by.s!.canStart, by.s!.canRestart, by.s!.canStop], [false, true, false]);
    presenter.request("n", "start");
    assert.deepEqual(source.acts, [{ verb: "start", serviceId: "n", taskId: TASK }]);
    assert.equal(rows(presenter).find((r) => r.id === "n")!.word, "starting…");
  });
});

describe("credenciais de acesso", () => {
  const withCredentials = () => service({ credentials: [{ label: "user", value: "ana" }, { label: "password", value: "s3cret-long-password" }] });

  test("masked with a fixed-length mask by default; show reveals one value, hide masks it again", async () => {
    const { presenter } = await panel(memoryServicesSource({ services: [withCredentials()] }));
    const [user, password] = rows(presenter)[0]!.credentials;
    assert.equal(user!.text, password!.text);
    assert.equal(user!.shown, false);
    presenter.toggleShown("svc-1", 1);
    assert.deepEqual(rows(presenter)[0]!.credentials.map((c) => c.text), [user!.text, "s3cret-long-password"]);
    presenter.toggleShown("svc-1", 1);
    assert.equal(rows(presenter)[0]!.credentials[1]!.text, user!.text);
  });

  test("the disclosure is closed by default, counts the credentials, and a row without any has none", async () => {
    const { presenter } = await panel(memoryServicesSource({ services: [withCredentials(), service({ id: "b", name: "z" })] }));
    assert.deepEqual(rows(presenter).map((r) => [r.credentialsCount, r.credentialsOpen]), [[2, false], [0, false]]);
    presenter.toggleCredentials("svc-1");
    assert.equal(rows(presenter)[0]!.credentialsOpen, true);
  });

  test("copy writes the value without revealing it, says Copied for 2s", async () => {
    const { presenter, copied, clock } = await panel(memoryServicesSource({ services: [withCredentials()] }));
    await presenter.copy("svc-1", 0);
    assert.deepEqual(copied, ["ana"]);
    assert.equal(rows(presenter)[0]!.credentials[0]!.shown, false);
    assert.equal(rows(presenter)[0]!.credentials[0]!.copied, true);
    clock.advance(2_000);
    assert.equal(rows(presenter)[0]!.credentials[0]!.copied, false);
  });

  test("no row attribute other than the shown text carries a value", async () => {
    const { presenter } = await panel(memoryServicesSource({ services: [withCredentials()] }));
    const row = JSON.stringify({ ...rows(presenter)[0], credentials: [] });
    assert.equal(row.includes("s3cret"), false);
    assert.equal(JSON.stringify(presenter.viewModel.selo).includes("ana"), false);
  });
});

describe("logs", () => {
  test("opening a log reads it, follows from where the read ended, and Escape goes back", async () => {
    const source = memoryServicesSource({ services: [service()] });
    source.logText = "[harness] started\nlistening\n";
    const { presenter } = await panel(source);
    presenter.openLog("svc-1");
    await settle();
    assert.equal(presenter.viewModel.logOpen, true);
    assert.deepEqual(source.logFollowedFrom, [28]);
    assert.deepEqual(presenter.log!.viewModel.lines.map((l) => l.tone), ["harness", "plain"]);
    assert.equal(presenter.key("Escape"), true);
    assert.equal(presenter.viewModel.logOpen, false);
    assert.equal(presenter.log, null);
  });

  test("live chunks extend it; a gap re-reads; scrolling up pauses following and resume brings it back", async () => {
    const source = memoryServicesSource({ services: [service()] });
    source.logText = "a\n";
    const { presenter } = await panel(source);
    presenter.openLog("svc-1");
    await settle();
    source.emitLog({ kind: "chunk", runId: "run-1", offset: 2, text: "b\n" });
    assert.deepEqual(presenter.log!.viewModel.lines.map((l) => l.text), ["a", "b"]);
    source.emitLog({ kind: "chunk", runId: "run-1", offset: 99, text: "x\n" });
    await settle();
    assert.equal(source.logReads, 2);
    presenter.log!.scrolled(false);
    assert.equal(presenter.log!.viewModel.following, false);
    presenter.log!.resume();
    assert.equal(presenter.log!.viewModel.following, true);
  });

  test("a log for a service that leaves the task's list closes", async () => {
    const source = memoryServicesSource({ services: [service()] });
    const { presenter } = await panel(source);
    presenter.openLog("svc-1");
    source.emit({ kind: "service_deleted", seq: 2, serviceId: "svc-1" });
    assert.equal(presenter.viewModel.logOpen, false);
  });
});
