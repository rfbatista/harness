import { assert, file, test } from "../../../shared/testing/test.js";
import {
  baseName,
  byName,
  changedFields,
  confirmsDelete,
  ignoredPathProblem,
  isAbsolutePath,
  matchesQuery,
  repositoryURL,
  sessionsRunning,
} from "./project.js";

file("projects/domain/project");

test("a path's last segment names what is not named", () => {
  assert.equal(baseName("/src/coding_pool/"), "coding_pool");
  assert.equal(baseName("~/work/harness"), "harness");
  assert.equal(baseName("  "), "");
});

test("paths must be absolute on the server's machine", () => {
  assert.ok(isAbsolutePath("/src/harness"));
  assert.ok(isAbsolutePath("~/src/harness"));
  assert.ok(isAbsolutePath(" ~ "), "the server expands a bare ~ to its user's home");
  assert.ok(!isAbsolutePath("~other/src"));
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

const summary = (id, name, rootDir = `/src/${name}`, running = 0) => ({
  project: { id, name, rootDir, ignoredPaths: [] },
  repositoryCount: 0,
  openTaskCount: 0,
  runningSessionCount: running,
  lastActivityAt: null,
});

test("projects sort by name ignoring case, then by id", () => {
  const list = [summary("p3", "beta"), summary("p2", "Alpha"), summary("p1", "alpha")].sort(byName);
  assert.deepEqual(list.map((s) => s.project.id), ["p1", "p2", "p3"]);
});

test("the filter matches the name or the directory, ignoring case and spaces around it", () => {
  const s = summary("p1", "Coding_Pool", "/Users/me/src/pool");
  assert.ok(matchesQuery(s, ""));
  assert.ok(matchesQuery(s, " coding "));
  assert.ok(matchesQuery(s, "SRC/POOL"));
  assert.ok(!matchesQuery(s, "harness"));
});

test("sessions running across projects add up", () => {
  assert.equal(sessionsRunning([summary("p1", "a", "/a", 2), summary("p2", "b", "/b", 1), summary("p3", "c")]), 3);
});

test("deleting asks for the project's name exactly, spaces around it aside", () => {
  assert.ok(confirmsDelete(" harness ", "harness"));
  assert.ok(!confirmsDelete("Harness", "harness"), "capitals count");
  assert.ok(!confirmsDelete("harn", "harness"));
  assert.ok(!confirmsDelete("", ""), "a nameless project still needs something typed");
});

test("an ignored path is relative to the project and names something inside it", () => {
  assert.equal(ignoredPathProblem("node_modules"), "");
  assert.equal(ignoredPathProblem("web/vendor/"), "");
  assert.ok(ignoredPathProblem("  "));
  assert.ok(ignoredPathProblem("."));
  assert.ok(ignoredPathProblem("../elsewhere"));
});

test("a settings save sends only the fields that changed, trimmed", () => {
  const saved = { id: "p1", name: "harness", rootDir: "/src/harness", ignoredPaths: [] };
  assert.deepEqual(changedFields(saved, { name: " harness ", rootDir: "/src/harness" }), {});
  assert.deepEqual(changedFields(saved, { name: "Harness", rootDir: "/src/harness" }), { name: "Harness" });
  assert.deepEqual(changedFields(saved, { name: "harness", rootDir: " ~/src/h " }), { rootDir: "~/src/h" });
  assert.deepEqual(changedFields(saved, { name: "", rootDir: "" }), {}, "empty keeps the saved value, as the server does");
});
