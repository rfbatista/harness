import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

function setup(opts: { repository?: string; repos?: boolean } = {}) {
  const store = fakeStore();
  if (opts.repos !== false) store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  const address = memoryScopeAddress(opts.repository ?? "");
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), address, projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/projects/p1/repositories/${id}/architecture/instructions` });
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
    assert.deepEqual(vm().layers.map((l) => l.title), ["Learn", "Score"]);
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

describe("saving a row", () => {
  test("setting a toggle sends one PATCH for the scope with that path, and the row reads its new source", async () => {
    const { presenter, row, store } = setup();
    await presenter.start();
    await presenter.change("learn.context_map.enabled", false);
    assert.ok(store.requests.includes("PATCH /api/projects/p1/agent-system"));
    assert.deepEqual([row("learn.context_map.enabled").value, row("learn.context_map.enabled").source.text], [false, "project"]);
  });

  test("Override in a repository saves the inherited value as an override at that level only", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    await presenter.start();
    await presenter.overrideHere("learn.context_map.enabled");
    assert.deepEqual(store.repositoryValues("r1"), { "learn.context_map.enabled": true });
    assert.deepEqual(store.project, {});
    assert.equal(row("learn.context_map.enabled").source.kind, "override");
  });

  test("Reset sends null for exactly that path", async () => {
    const { presenter, row, store } = setup({ repository: "r1" });
    store.repositoryValues("r1")["learn.context_map.enabled"] = false;
    store.repositoryValues("r1")["learn.task_memory.require_update"] = false;
    await presenter.start();
    await presenter.reset("learn.context_map.enabled");
    assert.deepEqual(store.repositoryValues("r1"), { "learn.task_memory.require_update": false });
    assert.equal(row("learn.context_map.enabled").source.text, "harness default");
  });

  test("a save says it applies to sessions started afterwards", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.change("learn.context_map.enabled", false);
    assert.match(vm().saved, /applies to sessions started afterwards/);
  });

  test("a refused save keeps the person's row as it was and says why with the code", async () => {
    const { presenter, vm, row, store } = setup();
    await presenter.start();
    store.refuseNextSave([{ field: "learn.context_map.enabled", code: "INVALID_TYPE", message: "takes true or false" }]);
    await presenter.change("learn.context_map.enabled", false);
    assert.equal(row("learn.context_map.enabled").value, true);
    assert.match(vm().problem, /INVALID_TYPE/);
  });

  test("changing scope after a save reads the repository's own levels", async () => {
    const { presenter, row } = setup();
    await presenter.start();
    await presenter.change("learn.context_map.enabled", false);
    await presenter.selectScope("r1");
    assert.deepEqual([row("learn.context_map.enabled").value, row("learn.context_map.enabled").source.text, row("learn.context_map.enabled").action], [false, "project", "override"]);
  });
});
