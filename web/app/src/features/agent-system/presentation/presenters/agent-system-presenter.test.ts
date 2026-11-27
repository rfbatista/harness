import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

function setup(opts: { repository?: string; repos?: boolean; clock?: { now(): number } } = {}) {
  const store = fakeStore();
  if (opts.repos !== false) store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  const address = memoryScopeAddress(opts.repository ?? "");
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), address, clock: opts.clock, projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/projects/p1/repositories/${id}/architecture/instructions` });
  const vm = () => presenter.viewModel;
  const row = (path: string) => vm().layers.flatMap((l) => l.rows).find((r) => r.path === path)!;
  return { store, address, presenter, vm, row };
}

describe("loading", () => {
  test("it shows the loading state until the project defaults arrive, then the Learn and Score layers", async () => {
    const { presenter, vm } = setup();
    assert.equal(vm().status, "loading");
    await presenter.start();
    assert.equal(vm().status, "ready");
    assert.deepEqual(vm().layers.map((l) => l.title), ["Learn", "Guard", "Review", "Score"]);
    assert.deepEqual(vm().scope, { kind: "project" });
  });

  test("an address naming a repository opens its scope", async () => {
    const { presenter, vm } = setup({ repository: "r2" });
    await presenter.start();
    assert.deepEqual(vm().scope, { kind: "repository", id: "r2" });
    assert.equal(vm().scopeName, "blog");
  });

  test("an address naming an unknown repository falls back to the project defaults", async () => {
    const { presenter, vm } = setup({ repository: "gone" });
    await presenter.start();
    assert.deepEqual(vm().scope, { kind: "project" });
  });

  test("a store that cannot be reached is a failed state with Retry", async () => {
    const { presenter, vm, store } = setup();
    store.breakNetwork();
    await presenter.start();
    assert.equal(vm().status, "failed");
    assert.match(vm().error, /offline|network/i);
  });
});

describe("the scope switch", () => {
  test("it lists the project defaults and each repository, with a red dot on a repository missing a required gate", async () => {
    const { presenter, vm, store } = setup();
    store.missingGates.set("r1", ["secret_scan"]);
    await presenter.start();
    assert.deepEqual(vm().repositories.map((r) => [r.id, r.red]), [["r1", true], ["r2", false]]);
  });

  test("choosing a repository writes ?repository=, reloads its rows, and choosing the project defaults clears it", async () => {
    const { presenter, vm, address } = setup();
    await presenter.start();
    await presenter.selectScope("r1");
    assert.deepEqual(address.history, ["r1"]);
    assert.deepEqual(vm().scope, { kind: "repository", id: "r1" });
    await presenter.selectScope("");
    assert.deepEqual(address.history, ["r1", ""]);
    assert.deepEqual(vm().scope, { kind: "project" });
  });

  test("Back and Forward follow the address without writing it again", async () => {
    const { presenter, vm, address } = setup();
    await presenter.start();
    await presenter.selectScope("r1");
    address.back("");
    await new Promise((r) => setTimeout(r, 0));
    assert.deepEqual(vm().scope, { kind: "project" });
    assert.deepEqual(address.history, ["r1"]);
  });

  test("a slow answer for a scope the person left is dropped", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    const slow = presenter.selectScope("r1");
    await presenter.selectScope("r2");
    await slow;
    assert.deepEqual(vm().scope, { kind: "repository", id: "r2" });
  });
});

describe("the ledger's sources and actions", () => {
  test("project scope: a harness default has no action; a value the project sets reads project with Reset", async () => {
    const { presenter, row, store } = setup();
    store.project["learn.task_memory.require_update"] = false;
    await presenter.start();
    assert.deepEqual([row("learn.context_map.enabled").source.text, row("learn.context_map.enabled").action], ["harness default", "none"]);
    assert.deepEqual([row("learn.task_memory.require_update").source.text, row("learn.task_memory.require_update").action, row("learn.task_memory.require_update").value], ["project", "reset", false]);
  });

  test("repository scope: an inherited row offers Override, an override offers Reset and names what it replaces", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    store.project["learn.task_memory.require_update"] = false;
    store.repositoryValues("r1")["learn.task_memory.require_update"] = true;
    await presenter.start();
    assert.equal(row("learn.context_map.enabled").action, "override");
    assert.equal(row("learn.task_memory.require_update").action, "reset");
    assert.deepEqual([row("learn.task_memory.require_update").source.kind, row("learn.task_memory.require_update").source.replaces], ["override", "project: off"]);
  });

  test("an override over a harness default names the harness default", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    store.repositoryValues("r1")["learn.context_map.enabled"] = false;
    await presenter.start();
    assert.equal(row("learn.context_map.enabled").source.replaces, "harness default: on");
  });

  test("the regenerate rule is read-only and the skills line differs by scope", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    const learn = vm().layers[0]!;
    assert.equal(learn.rows.find((r) => r.label === "Regenerate")?.kind, "fixed");
    assert.match(learn.note, /Choose a repository/);
    await presenter.selectScope("r1");
    assert.match(vm().layers[0]!.note, /instruction files/);
    assert.equal(vm().skillsHref, "/projects/p1/repositories/r1/architecture/instructions");
  });
});

describe("editing a draft", () => {
  test("a changed row counts as unsaved and is marked; nothing is sent until Save", async () => {
    const { presenter, vm, row, store } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("score.trajectory_recording.default", true);
    assert.equal(vm().unsaved, 2);
    assert.deepEqual([row("learn.context_map.enabled").value, row("learn.context_map.enabled").draft, row("learn.context_map.enabled").source.text], [false, true, "project"]);
    assert.ok(!store.requests.some((r) => r.startsWith("PATCH")));
  });

  test("changing a row back to what is saved is not a change", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("learn.context_map.enabled", true);
    assert.equal(vm().unsaved, 0);
  });

  test("Override in a repository drafts the inherited value as this level's own; Reset drafts null", async () => {
    const { presenter, vm, row, store } = setup({ repository: "r1" });
    store.repositoryValues("r1")["learn.task_memory.require_update"] = false;
    await presenter.start();
    presenter.overrideHere("learn.context_map.enabled");
    presenter.reset("learn.task_memory.require_update");
    assert.equal(vm().unsaved, 2);
    assert.deepEqual([row("learn.context_map.enabled").source.kind, row("learn.context_map.enabled").value], ["override", true]);
    assert.deepEqual([row("learn.task_memory.require_update").source.text, row("learn.task_memory.require_update").value], ["harness default", true]);
  });

  test("Discard restores every row and clears the count", async () => {
    const { presenter, vm, row } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("score.trajectory_recording.default", true);
    presenter.discard();
    assert.equal(vm().unsaved, 0);
    assert.deepEqual([row("learn.context_map.enabled").value, row("score.trajectory_recording.default").value], [true, false]);
  });

  test("switching scope drops the draft, which belongs to one level", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    await presenter.selectScope("r1");
    assert.equal(vm().unsaved, 0);
  });

  test("the save bar names its scope: project defaults, or overrides for the repository", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().saveLabel, "Save project defaults");
    await presenter.selectScope("r2");
    assert.equal(vm().saveLabel, "Save overrides for blog");
  });
});

describe("validation before save", () => {
  test("a value the form refuses lands on its row with code and fix, is listed by section and field, and Save is disabled", async () => {
    const { presenter, vm, row } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", "yes");
    assert.equal(row("learn.context_map.enabled").error?.code, "INVALID_TYPE");
    assert.deepEqual(vm().errors.map((e) => [e.section, e.label, e.code]), [["Learn", "Context map", "INVALID_TYPE"]]);
    assert.equal(vm().canSave, false);
    await presenter.save();
    assert.equal(vm().saved, "");
  });

  test("fixing the value clears the error and enables Save", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", "yes");
    presenter.edit("learn.context_map.enabled", false);
    assert.deepEqual(vm().errors, []);
    assert.equal(vm().canSave, true);
  });

  test("Save is disabled with nothing to save", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().canSave, false);
  });
});

describe("saving", () => {
  test("Save sends one PATCH of every drafted path for the scope, and the rows read their new source", async () => {
    const { presenter, vm, row, store } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("score.trajectory_recording.default", true);
    await presenter.save();
    assert.deepEqual(store.requests.filter((r) => r.startsWith("PATCH")), ["PATCH /api/projects/p1/agent-system"]);
    assert.deepEqual(store.project, { "learn.context_map.enabled": false, "score.trajectory_recording.default": true });
    assert.deepEqual([row("learn.context_map.enabled").source.text, row("learn.context_map.enabled").draft], ["project", false]);
    assert.equal(vm().unsaved, 0);
    assert.match(vm().saved, /applies to sessions started afterwards/);
  });

  test("the saved line carries its age, which tick() keeps current", async () => {
    let now = 1_000_000;
    const { presenter, vm } = setup({ clock: { now: () => now } });
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    await presenter.save();
    assert.equal(vm().saved, "Saved just now · applies to sessions started afterwards");
    now += 2 * 60_000 + 5_000;
    presenter.tick();
    assert.equal(vm().saved, "Saved 2 m ago · applies to sessions started afterwards");
    now += 2 * 60 * 60_000;
    presenter.tick();
    assert.equal(vm().saved, "Saved 2 h ago · applies to sessions started afterwards");
  });

  test("tick() leaves an empty saved line empty", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.tick();
    assert.equal(vm().saved, "");
  });

  test("Override in a repository saves the inherited value at that level only", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    await presenter.start();
    presenter.overrideHere("learn.context_map.enabled");
    await presenter.save();
    assert.deepEqual(store.repositoryValues("r1"), { "learn.context_map.enabled": true });
    assert.deepEqual(store.project, {});
    assert.equal(row("learn.context_map.enabled").source.kind, "override");
  });

  test("Reset saves null for exactly that path", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    store.repositoryValues("r1")["learn.context_map.enabled"] = false;
    store.repositoryValues("r1")["learn.task_memory.require_update"] = false;
    await presenter.start();
    presenter.reset("learn.context_map.enabled");
    await presenter.save();
    assert.deepEqual(store.repositoryValues("r1"), { "learn.task_memory.require_update": false });
    assert.equal(row("learn.context_map.enabled").source.text, "harness default");
  });

  test("a refused save (422) lands on the named rows with code and message, keeps the person's values and the draft, lists the codes, and disables Save until a value changes", async () => {
    const { presenter, vm, row, store } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("score.trajectory_recording.default", true);
    store.refuseNextSave([{ field: "score.trajectory_recording.default", code: "INVALID_TYPE", message: "takes true or false" }]);
    await presenter.save();
    assert.equal(row("score.trajectory_recording.default").error?.code, "INVALID_TYPE");
    assert.equal(row("score.trajectory_recording.default").error?.message, "takes true or false");
    assert.deepEqual([row("learn.context_map.enabled").value, row("score.trajectory_recording.default").value], [false, true]);
    assert.equal(vm().unsaved, 2);
    assert.deepEqual(vm().refused.map((e) => e.code), ["INVALID_TYPE"]);
    assert.equal(vm().canSave, false);
    assert.equal(vm().saved, "");
    presenter.edit("score.trajectory_recording.default", false);
    assert.equal(row("score.trajectory_recording.default").error, undefined);
    assert.deepEqual(vm().refused, []);
    assert.equal(vm().canSave, true);
  });

  test("a store that cannot be reached keeps the draft and says so, without field errors", async () => {
    const { presenter, vm, store } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    store.breakNetwork();
    await presenter.save();
    assert.equal(vm().unsaved, 1);
    assert.match(vm().problem, /NETWORK_ERROR/);
    assert.deepEqual(vm().refused, []);
    assert.equal(vm().canSave, true);
  });

  test("an edit after a save clears the saved line", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    await presenter.save();
    presenter.edit("learn.context_map.enabled", true);
    assert.equal(vm().saved, "");
  });

  test("changing scope after a save reads the repository's own levels", async () => {
    const { presenter, row } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    await presenter.save();
    await presenter.selectScope("r1");
    assert.deepEqual([row("learn.context_map.enabled").value, row("learn.context_map.enabled").source.text, row("learn.context_map.enabled").action], [false, "project", "override"]);
  });
});
