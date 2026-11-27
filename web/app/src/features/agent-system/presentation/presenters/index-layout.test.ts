// The index beside the rows, as the mockup draws it (settings-agent-system.html): per-layer override counts,
// the repository's own overrides, and the links to the repository's other pages.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

function setup(repository = "") {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), address: memoryScopeAddress(repository), projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/projects/p1/repositories/${id}/architecture` });
  return { store, presenter, vm: () => presenter.viewModel };
}

describe("the index at a repository's scope", () => {
  test("each layer counts the fields the repository overrides", async () => {
    const { store, presenter, vm } = setup("r1");
    store.repositoryValues("r1")["guard.gates.test.command"] = "make test";
    store.repositoryValues("r1")["review.critic.model"] = "opus";
    store.repositoryValues("r1")["review.visual_qa.ui"] = { start_command: "make run", url: "http://x", routes: ["/"], viewports: [], ui_paths: [] };
    await presenter.start();
    assert.deepEqual(vm().layers.map((l) => [l.key, l.overrides]), [["learn", 0], ["guard", 1], ["review", 2], ["score", 0]]);
  });

  test("it lists the repository's own overrides in words, in the layers' order", async () => {
    const { store, presenter, vm } = setup("r1");
    store.repositoryValues("r1")["review.critic.model"] = "opus";
    store.repositoryValues("r1")["guard.gates.test.command"] = "make test";
    store.repositoryValues("r1")["review.visual_qa.ui"] = { start_command: "make run", url: "http://x", routes: ["/"], viewports: [], ui_paths: [] };
    await presenter.start();
    assert.deepEqual(vm().ownOverrides, ["Guard · test command", "Review · critic model", "Review · UI declaration"]);
  });

  test("it links the repository's Architecture and Settings pages", async () => {
    const { presenter, vm } = setup("r1");
    await presenter.start();
    assert.deepEqual(vm().alsoHere, [
      { text: "Architecture", href: "/projects/p1/repositories/r1/architecture" },
      { text: "Settings: env, run commands", href: "/projects/p1/repositories/r1/env" },
    ]);
  });
});

describe("the index at the project's scope", () => {
  test("it has no own overrides and no links to a repository", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.deepEqual([vm().ownOverrides, vm().alsoHere], [[], []]);
    assert.ok(vm().layers.every((l) => l.overrides === 0));
  });
});
