// The contract every SettingsSource adapter keeps (Contract: Agent system settings store ↔ layers and web UI).

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { SettingsSource } from "../application/ports";
import { PROJECT_SCOPE, SettingsError, SettingsRefused } from "../domain/settings";
import type { FakeStore } from "./fake-store";

export interface SettingsSourceHarness {
  source: SettingsSource;
  store: FakeStore;
}

export function settingsSourceContract(name: string, make: () => SettingsSourceHarness): void {
  describe(`SettingsSource contract: ${name}`, () => {
    test("project defaults read as harness defaults where the project sets nothing", async () => {
      const { source } = make();
      const view = await source.view("p1", PROJECT_SCOPE);
      assert.deepEqual([view.effective.contextMapEnabled.value, view.effective.contextMapEnabled.source], [true, "harness"]);
      assert.deepEqual([view.effective.trajectoryRecordingDefault.value, view.effective.trajectoryRecordingDefault.source], [false, "harness"]);
      assert.deepEqual(view.values, {});
    });

    test("a repository's view says which level each value came from, and what it sets itself", async () => {
      const { source, store } = make();
      store.repositories.push({ id: "r1", name: "harness" });
      store.project["learn.task_memory.require_update"] = false;
      store.repositoryValues("r1")["learn.context_map.enabled"] = false;
      const view = await source.view("p1", { kind: "repository", id: "r1" });
      assert.deepEqual([view.effective.contextMapEnabled.value, view.effective.contextMapEnabled.source], [false, "repository"]);
      assert.deepEqual([view.effective.taskMemoryRequireUpdate.value, view.effective.taskMemoryRequireUpdate.source], [false, "project"]);
      assert.deepEqual(view.values, { "learn.context_map.enabled": false });
    });

    test("a repository missing a required gate reads red, and one that is not does not", async () => {
      const { source, store } = make();
      store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
      store.missingGates.set("r1", ["secret_scan"]);
      assert.deepEqual((await source.view("p1", { kind: "repository", id: "r1" })).effective.missingRequiredGates, ["secret_scan"]);
      assert.deepEqual((await source.view("p1", { kind: "repository", id: "r2" })).effective.missingRequiredGates, []);
    });

    test("the project's repositories, in the order the harness lists them", async () => {
      const { source, store } = make();
      store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
      assert.deepEqual(await source.repositories("p1"), [{ id: "r1", name: "harness" }, { id: "r2", name: "blog" }]);
    });

    test("a save sets a field and answers the level's new view; null resets it", async () => {
      const { source } = make();
      const saved = await source.save("p1", PROJECT_SCOPE, { "learn.context_map.enabled": false });
      assert.deepEqual([saved.effective.contextMapEnabled.value, saved.effective.contextMapEnabled.source], [false, "project"]);
      assert.deepEqual(saved.values, { "learn.context_map.enabled": false });
      const reset = await source.save("p1", PROJECT_SCOPE, { "learn.context_map.enabled": null });
      assert.deepEqual([reset.effective.contextMapEnabled.value, reset.effective.contextMapEnabled.source], [true, "harness"]);
    });

    test("a refused save fails with the store's errors by field path", async () => {
      const { source } = make();
      await assert.rejects(source.save("p1", PROJECT_SCOPE, { "learn.context_map.enabled": "yes" }), (err: unknown) => err instanceof SettingsRefused && err.errors[0]?.field === "learn.context_map.enabled" && err.errors[0].code === "INVALID_TYPE");
    });

    test("an unknown repository is NOT_FOUND", async () => {
      const { source } = make();
      await assert.rejects(source.view("p1", { kind: "repository", id: "nope" }), (err: unknown) => err instanceof SettingsError && err.code === "NOT_FOUND");
    });

    test("a network failure is NETWORK_ERROR", async () => {
      const { source, store } = make();
      store.breakNetwork();
      await assert.rejects(source.view("p1", PROJECT_SCOPE), (err: unknown) => err instanceof SettingsError && err.code === "NETWORK_ERROR");
    });
  });
}
