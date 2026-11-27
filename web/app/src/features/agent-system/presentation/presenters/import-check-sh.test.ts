// Import from check.sh on the Gates sub-list (ticket 06), through the presenter on the in-memory store.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

function setup(repository = "r1") {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" });
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), address: memoryScopeAddress(repository), projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/a/${id}` });
  const guard = () => presenter.viewModel.layers.find((l) => l.key === "guard")!;
  const gate = (kind: string) => guard().gates.find((g) => g.kind === kind)!;
  return { store, presenter, gate, vm: () => presenter.viewModel, patches: () => store.requests.filter((r) => r.startsWith("PATCH")) };
}

describe("Import from check.sh", () => {
  test("it offers the import at a repository's scope only", async () => {
    const repo = setup("r1");
    await repo.presenter.start();
    assert.equal(repo.vm().canImportCheckSh, true);
    const project = setup("");
    await project.presenter.start();
    assert.equal(project.vm().canImportCheckSh, false);
  });

  test("the answered commands fill the draft as overrides, counted as unsaved, and nothing is saved", async () => {
    const { presenter, store, gate, vm, patches } = setup();
    store.checkSh.set("r1", [
      { kind: "build", command: "./check.sh build", reason: "detected from check.sh" },
      { kind: "lint", command: "./check.sh lint", reason: "detected from check.sh" },
    ]);
    await presenter.start();
    await presenter.importFromCheckSh();
    assert.deepEqual([gate("build").command, gate("build").source.text, gate("build").draft], ["./check.sh build", "override", true]);
    assert.equal(gate("lint").command, "./check.sh lint");
    assert.equal(gate("test").command, "go test ./...");
    assert.equal(vm().unsaved, 2);
    assert.match(vm().importNote, /Imported 2 gates from check\.sh/);
    assert.deepEqual(patches(), []);
  });

  test("Save then sends the imported commands as one PATCH; Discard drops them", async () => {
    const { presenter, store, vm, patches } = setup();
    store.checkSh.set("r1", [{ kind: "test", command: "./check.sh", reason: "detected from check.sh" }]);
    await presenter.start();
    await presenter.importFromCheckSh();
    presenter.discard();
    assert.equal(vm().unsaved, 0);
    await presenter.importFromCheckSh();
    await presenter.save();
    assert.equal(patches().length, 1);
    assert.equal(store.repositoryValues("r1")["guard.gates.test.command"], "./check.sh");
  });

  test("an optional kind the script names reads on with its command", async () => {
    const { presenter, store, gate } = setup();
    store.checkSh.set("r1", [{ kind: "typecheck", command: "./check.sh typecheck", reason: "detected from check.sh" }]);
    await presenter.start();
    await presenter.importFromCheckSh();
    assert.deepEqual([gate("typecheck").on, gate("typecheck").command], [true, "./check.sh typecheck"]);
  });

  test("a script that names nothing says so and changes nothing", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.importFromCheckSh();
    assert.equal(vm().unsaved, 0);
    assert.match(vm().importNote, /no check\.sh|names no gate/i);
  });

  test("a command the repository already overrides to the same value is no change", async () => {
    const { presenter, store, vm } = setup();
    store.repositoryValues("r1")["guard.gates.build.command"] = "./check.sh build";
    store.checkSh.set("r1", [{ kind: "build", command: "./check.sh build", reason: "detected from check.sh" }]);
    await presenter.start();
    await presenter.importFromCheckSh();
    assert.equal(vm().unsaved, 0);
  });

  test("a failed import keeps the draft and says why", async () => {
    const { presenter, store, vm } = setup();
    await presenter.start();
    presenter.edit("guard.gates.test.command", "make test");
    store.breakNetwork();
    await presenter.importFromCheckSh();
    assert.equal(vm().unsaved, 1);
    assert.match(vm().importNote, /NETWORK_ERROR/);
  });
});
