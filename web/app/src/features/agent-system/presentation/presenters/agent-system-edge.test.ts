import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpSettingsFeed } from "../../infrastructure/http-settings-feed";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

/** The presenter on the fake store; a PATCH can be held until `release()`. */
function setup() {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  let hold: Promise<void> | null = null;
  let release: () => void = () => {};
  const fetch: FetchLike = async (url, init) => {
    if (init?.method === "PATCH" && hold !== null) await hold;
    return store.fetch(url, init);
  };
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(fetch)), feed: httpSettingsFeed(memoryFeed()), address: memoryScopeAddress(""), projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/a/${id}` });
  return {
    store,
    presenter,
    vm: () => presenter.viewModel,
    holdSaves: () => {
      hold = new Promise<void>((r) => (release = r));
    },
    release: () => release(),
  };
}

describe("a save in flight when the scope changes", () => {
  test("saving is not left stuck: the page can be edited and saved again after the stale answer lands", async () => {
    const { presenter, vm, holdSaves, release } = setup();
    await presenter.start();
    holdSaves();
    presenter.edit("guard.retry_limit", 5);
    const saving = presenter.save();
    assert.equal(vm().saving, true);
    await presenter.selectScope("r1"); // or back/forward: the same path
    release();
    await saving;
    assert.equal(vm().saving, false, "the stale answer must not leave the page saving");
    presenter.edit("guard.retry_limit", 7);
    assert.equal(vm().canSave, true);
    await presenter.save();
    assert.equal(vm().saved.startsWith("Saved"), true);
  });

  test("a stale answer is not applied to the scope now shown", async () => {
    const { presenter, vm, holdSaves, release } = setup();
    await presenter.start();
    holdSaves();
    presenter.edit("guard.retry_limit", 5);
    const saving = presenter.save();
    await presenter.selectScope("r1");
    release();
    await saving;
    assert.equal(vm().scope.kind, "repository");
    assert.equal(vm().saved, "", "the saved line belongs to the project scope, not this one");
  });

  test("a stale refusal also leaves saving off", async () => {
    const { presenter, vm, store, holdSaves, release } = setup();
    await presenter.start();
    store.refuseNextSave([{ field: "guard.retry_limit", code: "OUT_OF_RANGE", message: "1 to 10" }]);
    holdSaves();
    presenter.edit("guard.retry_limit", 50);
    const saving = presenter.save();
    await presenter.selectScope("r1");
    release();
    await saving;
    assert.equal(vm().saving, false);
  });
});

describe("Override on a toggle", () => {
  test("flipping it twice keeps the override: it is an override equal to the project value until Reset", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.selectScope("r1");
    presenter.overrideHere("learn.context_map.enabled");
    assert.equal(vm().unsaved, 1);
    presenter.edit("learn.context_map.enabled", false);
    presenter.edit("learn.context_map.enabled", true);
    assert.equal(vm().unsaved, 1, "the override stays in the draft");
    await presenter.save();
    const row = vm().layers.flatMap((l) => l.rows).find((r) => r.path === "learn.context_map.enabled");
    assert.equal(row?.source.kind, "override");
  });

  test("Reset drops it", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.selectScope("r1");
    presenter.overrideHere("learn.context_map.enabled");
    presenter.reset("learn.context_map.enabled");
    assert.equal(vm().unsaved, 0);
  });
});
