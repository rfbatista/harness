import { assert, test } from "../../../shared/testing/test.js";
import { makeSession } from "../testing/fixtures.js";
import { factsView } from "./sandboxView.js";

test("the Sandbox tab's repositories follow the recorded visibility in every mode", () => {
  const repos = (visibility, mode) => factsView(makeSession({ mode, sandbox: { sandboxed: true, visibility, driver: "seatbelt" } })).repositories;
  const own = [{ name: "this worktree", access: "read and write" }];
  for (const mode of ["implement", "design", ""]) {
    assert.deepEqual(repos("own repo", mode), own);
    assert.deepEqual(repos("all repos, read-only", mode), [...own, { name: "every repository of the project", access: "read-only" }]);
  }
  assert.deepEqual(repos("all repos, read-only", "architect"), [...own, { name: "every other repository of the project", access: "read-only" }]);
  assert.deepEqual(repos("all repos, read-only", "product"), [{ name: "every repository of the project", access: "read-only" }]);
  assert.equal(factsView(makeSession()).kind, "legacy");
});
