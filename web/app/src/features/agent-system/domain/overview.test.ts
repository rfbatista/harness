import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { isNewProject, overrideEntries, redEntries, type RepositoryState } from "./overview";
import type { SettingsView } from "./settings";

const view = (values: Record<string, unknown>, missing: string[] = []): SettingsView => ({ values, effective: { missingRequiredGates: missing } as unknown as SettingsView["effective"] });
const state = (id: string, name: string, v: SettingsView): RepositoryState => ({ repository: { id, name }, view: v });

describe("the project's overview of its repositories", () => {
  const states = [state("r1", "harness", view({ "guard.retry_limit": 5, "guard.gates.test.command": "make test" }, ["secret_scan"])), state("r2", "blog", view({})), state("r3", "cv", view({ "review.critic.model": "opus" }, ["lockfile", "secret_scan"]))];

  test("a repository overrides the fields it sets itself, in the order the store holds them", () => {
    assert.deepEqual(overrideEntries(states).map((e) => [e.repository.name, e.fields]), [["harness", ["guard.retry_limit", "guard.gates.test.command"]], ["cv", ["review.critic.model"]]]);
  });

  test("a repository is red with the required gates it misses, one that misses none is not listed", () => {
    assert.deepEqual(redEntries(states).map((e) => [e.repository.name, e.missing]), [["harness", ["secret_scan"]], ["cv", ["lockfile", "secret_scan"]]]);
  });

  test("a project is new when neither it nor any repository sets a field", () => {
    assert.equal(isNewProject(view({}), [state("r1", "harness", view({}))]), true);
    assert.equal(isNewProject(view({ "guard.retry_limit": 4 }), [state("r1", "harness", view({}))]), false);
    assert.equal(isNewProject(view({}), states), false);
  });
});
