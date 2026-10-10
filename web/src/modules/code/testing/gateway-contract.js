// The CodeGateway contract (../domain/ports.js), run by every implementation.
// A fake that passes it behaves like the real gateway as far as the code
// views can tell: the shapes of contract §3, the errors of §7, the pinned
// behaviour of §10 and the feed's code_changed of §8.

import { Codes } from "../../../shared/domain/errors.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { C2, HEAD, MAIN, makeWorld } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").CodeGateway} gateway
 * @property {(workspaceId: string, files: object, opts?: object) => void} change  edits a worktree as the server would see it, and announces it
 * @property {ReturnType<typeof import("../infrastructure/memory-gateway.js").memoryCode>} memory
 */

/** @param {string} name @param {(world: object) => Subject} makeSubject */
export function codeGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const ws = { workspaceId: "ws1" };
  const repo = { repositoryId: "r1" };

  contract("tree: a ref's root, directories first, with kinds, sizes and targets", async () => {
    const { gateway } = makeSubject(makeWorld());
    const t = await gateway.tree(repo, "");
    assert.deepEqual([t.source.kind, t.source.ref, t.source.commit, t.path], ["ref", "main", MAIN, ""]);
    assert.deepEqual(
      t.entries.map((e) => [e.name, e.kind]),
      [["assets", "dir"], ["docs", "dir"], ["internal", "dir"], ["third_party", "dir"], ["web", "dir"], [".gitkeep", "file"], ["README.md", "file"]],
    );
    const web = await gateway.tree(repo, "web");
    const link = web.entries.find((e) => e.name === "link.js");
    assert.deepEqual([link.kind, link.target, link.size], ["symlink", "app.js", null]);
    assert.deepEqual(web.entries.find((e) => e.name === "app.js").zones, ["z-web"]);
    assert.equal((await gateway.tree(repo, "third_party")).entries[0].kind, "submodule");
  });

  contract("tree: a worktree marks changes against its base, deleted files included", async () => {
    const { gateway } = makeSubject(makeWorld());
    const t = await gateway.tree(ws, "");
    const byName = Object.fromEntries(t.entries.map((e) => [e.name, e]));
    assert.deepEqual([t.source.kind, t.source.workspaceId, t.source.ref, t.source.commit], ["workspace", "ws1", "agent/visualize-code", HEAD]);
    assert.equal(byName.internal.change, "modified", "a directory with changes inside");
    assert.equal(byName.notes.change, "modified");
    assert.equal(byName["README.md"].change, "modified");
    assert.equal(byName.web.change, null);
    const docs = await gateway.tree(ws, "docs");
    assert.deepEqual(docs.entries.map((e) => [e.name, e.change, e.size]), [["old.md", "deleted", null]]);
    const notes = await gateway.tree(ws, "notes");
    assert.equal(notes.entries[0].change, "untracked");
  });

  contract("file: content, lines, language and zones; binary has none", async () => {
    const { gateway } = makeSubject(makeWorld());
    const f = await gateway.file(repo, "internal/feed/feed.go");
    assert.deepEqual([f.language, f.binary, f.truncated, f.lineCount, f.zones], ["go", false, false, 8, ["z-feed"]]);
    assert.ok(f.content.startsWith("package feed"));
    const b = await gateway.file(repo, "assets/logo.png");
    assert.deepEqual([b.binary, b.content, b.lineCount, b.size], [true, null, 0, 49_152]);
    const empty = await gateway.file(repo, ".gitkeep");
    assert.deepEqual([empty.content, empty.lineCount, empty.size], ["", 0, 0]);
  });

  contract("file: a symlink, a submodule, a directory and a deleted file are PATH_NOT_FOUND", async () => {
    const { gateway } = makeSubject(makeWorld());
    await assert.rejects(gateway.file(repo, "web/link.js"), Codes.PATH_NOT_FOUND);
    await assert.rejects(gateway.file(repo, "third_party/xterm"), Codes.PATH_NOT_FOUND);
    await assert.rejects(gateway.file(repo, "internal"), Codes.PATH_NOT_FOUND);
    await assert.rejects(gateway.file(ws, "docs/old.md"), Codes.PATH_NOT_FOUND);
  });

  contract("sources: one of repository or workspace; refs that do not resolve; unsafe paths", async () => {
    const { gateway } = makeSubject(makeWorld());
    await assert.rejects(gateway.tree({}, ""), Codes.INVALID_INPUT);
    await assert.rejects(gateway.tree({ repositoryId: "r1", ref: "main~1" }, ""), Codes.INVALID_INPUT);
    await assert.rejects(gateway.tree({ repositoryId: "r1", ref: "v2" }, ""), Codes.REF_NOT_FOUND);
    await assert.rejects(gateway.tree({ repositoryId: "empty" }, ""), Codes.REF_NOT_FOUND);
    await assert.rejects(gateway.tree({ repositoryId: "nope" }, ""), Codes.REPOSITORY_NOT_FOUND);
    await assert.rejects(gateway.tree({ workspaceId: "nope" }, ""), Codes.WORKSPACE_NOT_FOUND);
    await assert.rejects(gateway.file(repo, "../etc/passwd"), Codes.INVALID_INPUT);
    assert.equal((await gateway.tree({ repositoryId: "r1", ref: "v0.9.0" }, "")).entries.length, 1);
  });

  contract("paths: every file on disk, deleted ones left out", async () => {
    const { gateway } = makeSubject(makeWorld());
    const p = await gateway.paths(ws);
    assert.equal(p.truncated, false);
    assert.ok(p.paths.includes("notes/todo.md"));
    assert.ok(!p.paths.includes("docs/old.md"));
    assert.ok(!p.paths.includes("web/link.js"), "only files");
  });

  contract("diff: scope all is the base against disk; branch is committed only; working is uncommitted", async () => {
    const { gateway } = makeSubject(makeWorld());
    const all = await gateway.diff({ workspaceId: "ws1" });
    assert.deepEqual([all.scope, all.base], ["all", MAIN]);
    assert.deepEqual(all.files.map((f) => [f.path, f.status]), [
      ["README.md", "modified"],
      ["docs/old.md", "deleted"],
      ["internal/code/browser.go", "added"],
      ["internal/feed/feed.go", "modified"],
      ["notes/todo.md", "untracked"],
    ]);
    const feed = all.files.find((f) => f.path === "internal/feed/feed.go");
    assert.deepEqual([feed.added, feed.deleted, feed.language, feed.zones], [1, 0, "go", ["z-feed"]]);
    const added = feed.hunks[0].lines.find((l) => l.kind === "add");
    assert.deepEqual([added.text, added.old, typeof added.new], ["\tctx.Done()", null, "number"]);
    const branch = await gateway.diff({ workspaceId: "ws1", scope: "branch" });
    assert.deepEqual(branch.files.map((f) => f.path), ["docs/old.md", "internal/code/browser.go", "internal/feed/feed.go"]);
    const working = await gateway.diff({ workspaceId: "ws1", scope: "working" });
    assert.deepEqual([working.base, working.files.map((f) => [f.path, f.status])], [HEAD, [["README.md", "modified"], ["notes/todo.md", "untracked"]]]);
    assert.equal(all.added, all.files.reduce((s, f) => s + Math.max(0, f.added), 0));
  });

  contract("diff: one commit against its parent; path narrows it", async () => {
    const { gateway } = makeSubject(makeWorld());
    const d = await gateway.diff({ repositoryId: "r1", commit: C2 });
    assert.deepEqual([d.scope, d.files.map((f) => f.path)], ["commit", ["README.md", "web/app.js"]]);
    const one = await gateway.diff({ repositoryId: "r1", commit: C2, path: "README.md" });
    assert.equal(one.files.length, 1);
    await assert.rejects(gateway.diff({ repositoryId: "r1", commit: "f".repeat(40) }), Codes.COMMIT_NOT_FOUND);
    await assert.rejects(gateway.diff({ repositoryId: "r1", commit: C2, path: "nope.go" }), Codes.PATH_NOT_FOUND);
  });

  contract("diff: a file past the limits is withheld, and comes with path=", async () => {
    const world = makeWorld({ limits: { perFile: 5 } });
    const { gateway } = makeSubject(world);
    const d = await gateway.diff({ workspaceId: "ws1", scope: "branch" });
    const old = d.files.find((f) => f.path === "docs/old.md");
    assert.deepEqual([old.withheld, old.hunks, old.deleted], ["too_large", null, 60]);
    const one = await gateway.diff({ workspaceId: "ws1", scope: "branch", path: "docs/old.md" });
    assert.equal(one.files[0].hunks.length, 1);
  });

  contract("a gone worktree: browsing is WORKSPACE_MISSING; branch still answers, marked gone", async () => {
    const world = makeWorld();
    const subject = makeSubject(world);
    subject.memory.gone("ws1");
    await assert.rejects(subject.gateway.tree(ws, ""), Codes.WORKSPACE_MISSING);
    await assert.rejects(subject.gateway.diff({ workspaceId: "ws1" }), Codes.WORKSPACE_MISSING);
    const branch = await subject.gateway.diff({ workspaceId: "ws1", scope: "branch" });
    assert.equal(branch.source.gone, true);
  });

  contract("a gone base: all and branch are REF_NOT_FOUND, working still answers", async () => {
    const subject = makeSubject(makeWorld());
    subject.memory.baseGone("ws1");
    await assert.rejects(subject.gateway.diff({ workspaceId: "ws1" }), Codes.REF_NOT_FOUND);
    await assert.rejects(subject.gateway.diff({ workspaceId: "ws1", scope: "branch" }), Codes.REF_NOT_FOUND);
    assert.equal((await subject.gateway.diff({ workspaceId: "ws1", scope: "working" })).files.length, 2);
    const t = await subject.gateway.tree(ws, "");
    assert.ok(t.entries.every((e) => e.change === null), "browsing never fails on a gone base");
  });

  contract("map: contexts by name with their zones, loose zones, unzoned files", async () => {
    const { gateway } = makeSubject(makeWorld());
    const m = await gateway.map(repo);
    assert.deepEqual(m.contexts.map((c) => [c.name, c.fileCount]), [["Web", 1], ["Workspaces", 2]]);
    assert.deepEqual(m.contexts[1].zones.map((z) => [z.name, z.pattern, z.fileCount]), [["feed", "internal/feed/**", 1], ["ports", "internal/ports/**", 1]]);
    assert.deepEqual(m.unassignedZones.map((z) => [z.name, z.fileCount]), [["docs", 1]]);
    assert.deepEqual([m.fileCount, m.unzonedFileCount], [7, 3]);
  });

  contract("follow: code_changed of the project's worktrees, until stopped", async () => {
    const subject = makeSubject(makeWorld());
    const seen = [];
    const stop = subject.gateway.follow("p1", (c) => seen.push(c), () => {});
    await flush();
    subject.change("ws1", { "web/app.js": "changed\n" });
    await flush();
    assert.deepEqual(seen, [{ workspaceId: "ws1", head: HEAD }]);
    stop();
    subject.change("ws1", { "web/app.js": "again\n" });
    await flush();
    assert.equal(seen.length, 1);
  });
}
