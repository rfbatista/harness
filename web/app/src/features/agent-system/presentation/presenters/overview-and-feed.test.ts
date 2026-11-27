import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpSettingsFeed } from "../../infrastructure/http-settings-feed";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

const FEED = "/events?project_id=p1";

function setup(opts: { repository?: string } = {}) {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  const feed = memoryFeed();
  const presenter = new AgentSystemPresenter({ source: httpSettingsSource(apiClient(store.fetch)), feed: httpSettingsFeed(feed), address: memoryScopeAddress(opts.repository ?? ""), projectId: "p1", projectName: "pessoal", architectureHref: (id) => `/a/${id}` });
  const vm = () => presenter.viewModel;
  /** Another page saved: the store holds it, and the feed says so. */
  const savedElsewhere = async (repositoryId = "") => {
    feed.emit(FEED, { agent_system_settings: { project_id: "p1", ...(repositoryId === "" ? {} : { repository_id: repositoryId }) } });
    await presenter.settled();
  };
  return { store, feed, presenter, vm, savedElsewhere };
}

describe("the overview of the project", () => {
  test("a project that saved nothing is new: every value is a harness default", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().overview.newProject, true);
    assert.deepEqual(vm().overview.overrides, []);
  });

  test("it lists the fields each repository overrides and the repositories that are red with their missing kinds", async () => {
    const { presenter, vm, store } = setup();
    store.repositoryValues("r1")["guard.retry_limit"] = 5;
    store.repositoryValues("r1")["review.critic.model"] = "opus";
    store.missingGates.set("r2", ["lockfile", "secret_scan"]);
    await presenter.start();
    assert.equal(vm().overview.newProject, false);
    assert.deepEqual(vm().overview.overrides.map((o) => [o.repositoryId, o.name, o.fields]), [["r1", "harness", ["guard.retry_limit", "review.critic.model"]]]);
    assert.deepEqual(vm().overview.red.map((r) => [r.repositoryId, r.name, r.missing]), [["r2", "blog", ["Lockfile", "Secret scan"]]]);
  });

  test("a project default makes the project no longer new", async () => {
    const { presenter, vm, store } = setup();
    store.project["guard.retry_limit"] = 4;
    await presenter.start();
    assert.equal(vm().overview.newProject, false);
  });

  test("saving a repository's override shows in the overview", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.selectScope("r1");
    presenter.edit("guard.retry_limit", 6);
    await presenter.save();
    assert.deepEqual(vm().overview.overrides.map((o) => o.repositoryId), ["r1"]);
    assert.equal(vm().overview.newProject, false);
  });
});

describe("settings changed on another page", () => {
  test("a save elsewhere raises the banner, and Reload refreshes the rows", async () => {
    const { presenter, vm, store, savedElsewhere } = setup();
    await presenter.start();
    store.project["guard.retry_limit"] = 7;
    await savedElsewhere();
    assert.equal(vm().changedElsewhere, true);
    assert.equal(vm().layers.flatMap((l) => l.rows).find((r) => r.path === "guard.retry_limit")?.current, 3, "the rows are not replaced under the person's hands");
    await presenter.reload();
    assert.equal(vm().changedElsewhere, false);
    assert.equal(vm().layers.flatMap((l) => l.rows).find((r) => r.path === "guard.retry_limit")?.current, 7);
  });

  test("Reload keeps the person's unsaved rows", async () => {
    const { presenter, vm, store, savedElsewhere } = setup();
    await presenter.start();
    presenter.edit("learn.context_map.enabled", false);
    store.project["guard.retry_limit"] = 7;
    await savedElsewhere();
    await presenter.reload();
    assert.equal(vm().unsaved, 1);
    assert.equal(vm().layers.flatMap((l) => l.rows).find((r) => r.path === "learn.context_map.enabled")?.current, false);
    assert.equal(vm().layers.flatMap((l) => l.rows).find((r) => r.path === "guard.retry_limit")?.current, 7);
  });

  test("the page's own save does not raise the banner, whenever its echo arrives", async () => {
    const { presenter, vm, savedElsewhere } = setup();
    await presenter.start();
    presenter.edit("guard.retry_limit", 5);
    await presenter.save();
    await savedElsewhere();
    assert.equal(vm().changedElsewhere, false);
  });

  test("an echo that arrives before the save is answered does not raise the banner either", async () => {
    const { presenter, vm, feed } = setup();
    await presenter.start();
    presenter.edit("guard.retry_limit", 5);
    const saving = presenter.save();
    feed.emit(FEED, { agent_system_settings: { project_id: "p1" } });
    await saving;
    await presenter.settled();
    assert.equal(vm().changedElsewhere, false);
  });

  test("a change at a repository the page does not show leaves the rows and updates the overview", async () => {
    const { presenter, vm, store, savedElsewhere } = setup();
    await presenter.start();
    store.repositoryValues("r2")["guard.retry_limit"] = 2;
    await savedElsewhere("r2");
    assert.equal(vm().changedElsewhere, false);
    assert.deepEqual(vm().overview.overrides.map((o) => o.repositoryId), ["r2"]);
  });

  test("a change at the shown repository, or at the project while a repository shows, raises the banner", async () => {
    const { presenter, vm, store, savedElsewhere } = setup({ repository: "r1" });
    await presenter.start();
    store.repositoryValues("r1")["guard.retry_limit"] = 2;
    await savedElsewhere("r1");
    assert.equal(vm().changedElsewhere, true);
    await presenter.reload();
    store.project["review.critic.enabled"] = false;
    await savedElsewhere();
    assert.equal(vm().changedElsewhere, true);
  });

  test("a feed that says nothing changed in what is shown raises nothing", async () => {
    const { presenter, vm, savedElsewhere } = setup();
    await presenter.start();
    await savedElsewhere();
    assert.equal(vm().changedElsewhere, false);
  });

  test("it stops following the feed when the page is taken down", async () => {
    const { presenter, feed } = setup();
    await presenter.start();
    assert.equal(feed.followers(FEED), 1);
    presenter.stop();
    assert.equal(feed.followers(FEED), 0);
  });
});
