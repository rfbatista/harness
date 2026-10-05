import { assert, file, test } from "../../../shared/testing/test.js";
import { baseName, isAbsolutePath, repositoryURL } from "./project.js";

file("projects/domain/project");

test("a path's last segment names what is not named", () => {
  assert.equal(baseName("/src/coding_pool/"), "coding_pool");
  assert.equal(baseName("~/work/harness"), "harness");
  assert.equal(baseName("  "), "");
});

test("paths must be absolute on the server's machine", () => {
  assert.ok(isAbsolutePath("/src/harness"));
  assert.ok(isAbsolutePath("~/src/harness"));
  assert.ok(!isAbsolutePath("src/harness"));
});

test("a repository without a remote is recorded by its path", () => {
  assert.equal(repositoryURL("", "/src/harness"), "file:///src/harness");
  assert.equal(repositoryURL(" git@github.com:me/h.git ", "/src/harness"), "git@github.com:me/h.git");
});

test("found checkouts not yet added, and paths relative to the project", async () => {
  const { notYetAdded, relativeTo } = await import("./project.js");
  const found = [{ path: "/p/api" }, { path: "/p/web/" }];
  assert.deepEqual(notYetAdded(found, [{ rootDir: "/p/web" }]).map((f) => f.path), ["/p/api"]);
  assert.equal(relativeTo("/p/", "/p/libs/kit"), "libs/kit");
  assert.equal(relativeTo("/p", "/p"), ".");
  assert.equal(relativeTo("/p", "/elsewhere/x"), "/elsewhere/x");
});

test("env file paths stay inside the repository and out of .git", async () => {
  const { envPathProblem } = await import("./project.js");
  for (const ok of [".env", "apps/api/.env", "./config/.env.local", "a/../b/.env", "apps\\web\\.env"]) {
    assert.equal(envPathProblem(ok), "", ok);
  }
  for (const bad of ["", "/etc/hosts", "~/.env", "../.env", "a/../../.env", ".git/config", ".", "a/.."]) {
    assert.ok(envPathProblem(bad), bad);
  }
});
