// The Guard section (ticket 03), through the presenter on the in-memory store.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

function setup(opts: { repository?: string } = {}) {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), address: memoryScopeAddress(opts.repository ?? ""), projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/a/${id}` });
  const guard = () => presenter.viewModel.layers.find((l) => l.key === "guard")!;
  const gate = (kind: string) => guard().gates.find((g) => g.kind === kind)!;
  const row = (path: string) => guard().rows.find((r) => r.path === path)!;
  const patches = () => store.requests.filter((r) => r.startsWith("PATCH"));
  return { store, presenter, vm: () => presenter.viewModel, guard, gate, row, patches };
}

describe("the Gates sub-list", () => {
  test("it lists the five required kinds then the three optional ones, with command, timeout and source reason", async () => {
    const { presenter, guard, gate } = setup();
    await presenter.start();
    assert.deepEqual(guard().gates.map((g) => g.kind), ["build", "test", "lint", "secret_scan", "lockfile", "typecheck", "sast", "dependency_scan"]);
    assert.deepEqual([gate("build").command, gate("build").timeout, gate("build").source.text, gate("build").source.reason], ["go build ./...", 600, "harness default", "detected from go.mod"]);
    assert.deepEqual(guard().gates.map((g) => g.required), [true, true, true, true, true, false, false, false]);
  });

  test("an optional kind nothing sets is off, with its suggestion as the placeholder; a required kind is always on", async () => {
    const { presenter, gate } = setup();
    await presenter.start();
    assert.equal(gate("build").on, true);
    assert.deepEqual([gate("typecheck").on, gate("typecheck").command, gate("typecheck").placeholder], [false, "", "not set for this stack"]);
  });

  test("an optional kind the project sets reads on with the project's command", async () => {
    const { presenter, gate, store } = setup();
    store.project["guard.gates.sast.command"] = "gosec ./...";
    await presenter.start();
    assert.deepEqual([gate("sast").on, gate("sast").command, gate("sast").source.text, gate("sast").action], [true, "gosec ./...", "project", "reset"]);
  });

  test("editing a command drafts the contract's path and Save sends only that path; Reset sends null", async () => {
    const { presenter, gate, vm, store } = setup();
    await presenter.start();
    presenter.edit("guard.gates.test.command", "make test");
    assert.deepEqual([gate("test").command, gate("test").source.text, gate("test").draft, vm().unsaved], ["make test", "project", true, 1]);
    await presenter.save();
    assert.equal(store.project["guard.gates.test.command"], "make test");
    assert.deepEqual([gate("test").source.text, gate("test").action], ["project", "reset"]);
    presenter.resetGate("test");
    await presenter.save();
    assert.ok(!("guard.gates.test.command" in store.project));
    assert.equal(gate("test").source.text, "harness default");
  });

  test("editing the timeout drafts guard.gates.<kind>.timeout_seconds as a number", async () => {
    const { presenter, store } = setup();
    await presenter.start();
    presenter.editNumber("guard.gates.build.timeout_seconds", "900");
    await presenter.save();
    assert.equal(store.project["guard.gates.build.timeout_seconds"], 900);
  });

  test("an optional kind's switch sends guard.gates.<kind>.enabled", async () => {
    const { presenter, gate, store } = setup();
    store.project["guard.gates.sast.command"] = "gosec ./...";
    await presenter.start();
    presenter.edit("guard.gates.sast.enabled", false);
    assert.equal(gate("sast").on, false);
    await presenter.save();
    assert.equal(store.project["guard.gates.sast.enabled"], false);
  });

  test("a required kind offers no switch: its enabled path is never a field of the form", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.edit("guard.gates.lint.enabled", false);
    // The form does not produce it; were it sent, the store's REQUIRED_GATE_DISABLED lands on the page.
    await presenter.save();
    assert.ok(vm().refused.some((e) => e.code === "REQUIRED_GATE_DISABLED"));
  });

  test("an emptied required command is refused inline, never sent, and Save stays disabled", async () => {
    const { presenter, vm, gate, patches } = setup();
    await presenter.start();
    presenter.edit("guard.gates.build.command", "");
    assert.match(gate("build").errors.command?.message ?? "", /Reset the field to inherit/);
    assert.equal(vm().canSave, false);
    await presenter.save();
    assert.equal(patches().length, 0);
  });

  test("an emptied optional command is refused inline too", async () => {
    const { presenter, gate } = setup();
    await presenter.start();
    presenter.edit("guard.gates.typecheck.command", "");
    assert.ok(gate("typecheck").errors.command);
  });

  test("a timeout out of range or not a number is refused inline", async () => {
    const { presenter, gate } = setup();
    await presenter.start();
    presenter.editNumber("guard.gates.build.timeout_seconds", "0");
    assert.equal(gate("build").errors.timeout?.code, "OUT_OF_RANGE");
    presenter.editNumber("guard.gates.build.timeout_seconds", "ten");
    assert.equal(gate("build").errors.timeout?.code, "INVALID_TYPE");
    assert.equal(gate("build").timeout, "ten");
  });

  test("repository scope: an inherited gate offers Override; Override drafts its command and timeout; an override names what it replaces", async () => {
    const { presenter, gate, vm, store } = setup({ repository: "r1" });
    store.project["guard.gates.test.command"] = "go test ./... -count=1";
    await presenter.start();
    assert.deepEqual([gate("test").source.text, gate("test").action], ["project", "override"]);
    presenter.overrideGate("test");
    assert.equal(vm().unsaved, 2);
    assert.deepEqual([gate("test").source.kind, gate("test").source.replaces, gate("test").action], ["override", "project: go test ./... -count=1", "reset"]);
    await presenter.save();
    assert.deepEqual(store.repositoryValues("r1"), { "guard.gates.test.command": "go test ./... -count=1", "guard.gates.test.timeout_seconds": 600 });
    presenter.resetGate("test");
    await presenter.save();
    assert.deepEqual(store.repositoryValues("r1"), {});
  });
});

describe("a repository missing a required gate", () => {
  test("it is red: the gate reads missing on its row, the banner names kind and detection's reason, the index counts it", async () => {
    const s = setup({ repository: "r1" });
    s.store.missingGates.set("r1", ["secret_scan"]);
    await s.presenter.start();
    assert.deepEqual(s.vm().missingGates, [{ kind: "secret_scan", label: "Secret scan", reason: "gitleaks not installed" }]);
    assert.deepEqual([s.gate("secret_scan").missing, s.gate("secret_scan").source.kind, s.gate("secret_scan").source.text, s.gate("secret_scan").required], [true, "missing", "no command at any level", true]);
    assert.equal(s.guard().attention, 1);
  });

  test("typing a command clears the missing state; Save sends it", async () => {
    const s = setup({ repository: "r1" });
    s.store.missingGates.set("r1", ["secret_scan"]);
    await s.presenter.start();
    s.presenter.edit("guard.gates.secret_scan.command", "gitleaks detect");
    assert.equal(s.gate("secret_scan").missing, false);
    assert.equal(s.vm().missingGates.length, 0);
    await s.presenter.save();
    assert.equal(s.store.repositoryValues("r1")["guard.gates.secret_scan.command"], "gitleaks detect");
  });

  test("the project's Guard index counts the red repositories; a green project shows none", async () => {
    const s = setup();
    await s.presenter.start();
    assert.equal(s.guard().attention, 0);
    const red = setup();
    red.store.missingGates.set("r1", ["secret_scan"]);
    red.store.missingGates.set("r2", ["lockfile"]);
    await red.presenter.start();
    assert.equal(red.guard().attention, 2);
    assert.equal(red.vm().missingGates.length, 0);
  });
});

describe("enforcement rows", () => {
  test("protected paths are chips with the harness default list; adding and removing one round-trips through the PATCH", async () => {
    const { presenter, row, store } = setup();
    await presenter.start();
    assert.deepEqual(row("guard.protected_paths").items, ["check.sh", ".github/workflows/**", ".gitlab-ci.yml"]);
    presenter.edit("guard.protected_paths", [...row("guard.protected_paths").items, "**/*.lock"]);
    await presenter.save();
    assert.deepEqual(store.project["guard.protected_paths"], ["check.sh", ".github/workflows/**", ".gitlab-ci.yml", "**/*.lock"]);
    presenter.edit("guard.protected_paths", row("guard.protected_paths").items.filter((g) => g !== "check.sh"));
    await presenter.save();
    assert.deepEqual(store.project["guard.protected_paths"], [".github/workflows/**", ".gitlab-ci.yml", "**/*.lock"]);
  });

  test("a malformed glob is marked on its chip and refused inline with INVALID_GLOB naming it", async () => {
    const { presenter, row, vm } = setup();
    await presenter.start();
    presenter.edit("guard.protected_paths", ["check.sh", "src/[**"]);
    assert.deepEqual(row("guard.protected_paths").invalid, ["src/[**"]);
    assert.equal(row("guard.protected_paths").error?.code, "INVALID_GLOB");
    assert.match(row("guard.protected_paths").error?.message ?? "", /src\/\[\*\*/);
    assert.equal(vm().canSave, false);
  });

  test("a glob the store refuses lands on the same row and keeps the chips", async () => {
    const { presenter, row, vm, store } = setup();
    await presenter.start();
    presenter.edit("guard.protected_paths", ["check.sh"]);
    store.refuseNextSave([{ field: "guard.protected_paths", code: "INVALID_GLOB", message: "check.sh is not a glob the store takes" }]);
    await presenter.save();
    assert.equal(row("guard.protected_paths").error?.code, "INVALID_GLOB");
    assert.deepEqual(row("guard.protected_paths").items, ["check.sh"]);
    assert.equal(vm().unsaved, 1);
  });

  test("the lockfile rule and tests fail first are switches with their own words", async () => {
    const { presenter, row, store } = setup();
    await presenter.start();
    assert.deepEqual(row("guard.tests_fail_first.available").words, ["Not available", "Available"]);
    presenter.edit("guard.lockfile_rule.enabled", false);
    presenter.edit("guard.tests_fail_first.available", true);
    await presenter.save();
    assert.deepEqual([store.project["guard.lockfile_rule.enabled"], store.project["guard.tests_fail_first.available"]], [false, true]);
  });

  test("the retry limit is a number; 11 and 0 are refused inline with the range, text with INVALID_TYPE; 5 saves", async () => {
    const { presenter, row, vm, store, patches } = setup();
    await presenter.start();
    assert.deepEqual([row("guard.retry_limit").current, row("guard.retry_limit").unit], [3, "red runs in a row"]);
    presenter.editNumber("guard.retry_limit", "11");
    assert.deepEqual([row("guard.retry_limit").error?.code, row("guard.retry_limit").error?.message], ["OUT_OF_RANGE", "Takes 1–10."]);
    assert.equal(vm().canSave, false);
    presenter.editNumber("guard.retry_limit", "0");
    assert.equal(row("guard.retry_limit").error?.code, "OUT_OF_RANGE");
    presenter.editNumber("guard.retry_limit", "3.5");
    assert.equal(row("guard.retry_limit").error?.code, "INVALID_TYPE");
    assert.equal(patches().length, 0);
    presenter.editNumber("guard.retry_limit", "5");
    assert.equal(row("guard.retry_limit").error, undefined);
    await presenter.save();
    assert.equal(store.project["guard.retry_limit"], 5);
  });

  test("an override of the retry limit in a repository names the harness default and Reset sends null", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    await presenter.start();
    presenter.overrideHere("guard.retry_limit");
    presenter.editNumber("guard.retry_limit", "6");
    assert.deepEqual([row("guard.retry_limit").source.kind, row("guard.retry_limit").source.replaces], ["override", "harness default: 3"]);
    await presenter.save();
    assert.equal(store.repositoryValues("r1")["guard.retry_limit"], 6);
    presenter.reset("guard.retry_limit");
    await presenter.save();
    assert.ok(!("guard.retry_limit" in store.repositoryValues("r1")));
  });

  test("the errors of Guard rows are listed in the save bar by section and field", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.editNumber("guard.retry_limit", "99");
    presenter.edit("guard.gates.test.command", "");
    assert.deepEqual(vm().errors.map((e) => `${e.section} › ${e.label}`).sort(), ["Guard › Retry limit", "Guard › Test command"]);
  });
});
