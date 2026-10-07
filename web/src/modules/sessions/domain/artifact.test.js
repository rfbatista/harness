import { assert, file, test } from "../../../shared/testing/test.js";
import { A0, makeArtifact } from "../testing/artifact-fixtures.js";
import {
  applyChange,
  applyProjectChange,
  applyPublish,
  attachableAssets,
  attachableTasks,
  belongsTo,
  byUpdated,
  fileName,
  isKnown,
  isLoopbackUrl,
  isNewer,
  isNotOlder,
  isPromotable,
  isPublish,
  KINDS,
  relationTo,
  Scope,
  withoutArtifact,
} from "./artifact.js";

file("sessions/domain/artifact");

const later = (s) => new Date(A0.getTime() + s * 1000);

test("the kinds are the contract's five", () => {
  assert.deepEqual([...KINDS], ["page", "image", "video", "url", "file"]);
});

test("a publish puts the artifact first; a re-publish replaces its earlier revision", () => {
  const old = makeArtifact({ id: "a1", revision: 1 });
  const other = makeArtifact({ id: "a2", updatedAt: later(10) });
  const list = applyPublish([other], old);
  assert.deepEqual(list.map((a) => a.id), ["a2", "a1"], "ordered by updated_at, newest first");
  const bumped = makeArtifact({ id: "a1", revision: 2, updatedAt: later(20) });
  assert.equal(isKnown(list, bumped), true);
  const next = applyPublish(list, bumped);
  assert.deepEqual(next.map((a) => `${a.id}@${a.revision}`), ["a1@2", "a2@1"]);
  assert.equal(isKnown(next, makeArtifact({ id: "a9" })), false);
});

test("byUpdated sorts newest first without mutating", () => {
  const a = makeArtifact({ id: "a" });
  const b = makeArtifact({ id: "b", updatedAt: later(1) });
  const input = [a, b];
  assert.deepEqual(byUpdated(input).map((x) => x.id), ["b", "a"]);
  assert.deepEqual(input.map((x) => x.id), ["a", "b"]);
});

test("only http(s) on this machine may be embedded", () => {
  assert.equal(isLoopbackUrl("http://localhost:5173/"), true);
  assert.equal(isLoopbackUrl("http://127.0.0.1:3000/app"), true);
  assert.equal(isLoopbackUrl("https://[::1]:8443/"), true);
  assert.equal(isLoopbackUrl("http://example.com/"), false);
  assert.equal(isLoopbackUrl("http://localhost.evil.com/"), false);
  assert.equal(isLoopbackUrl("javascript:alert(1)"), false);
  assert.equal(isLoopbackUrl("file:///etc/passwd"), false);
  assert.equal(isLoopbackUrl("not a url"), false);
  assert.equal(isLoopbackUrl(""), false);
});

test("a file's name is its last path segment", () => {
  assert.equal(fileName(makeArtifact({ path: "out/report.pdf" })), "report.pdf");
  assert.equal(fileName(makeArtifact({ kind: "url", path: "", url: "http://localhost:3000" })), "");
});

test("an artifact is newer with a higher revision, or the same revision updated later (a move)", () => {
  const known = makeArtifact({ id: "a1", revision: 2, updatedAt: later(10) });
  assert.equal(isNewer(known, makeArtifact({ id: "a1", revision: 3, updatedAt: later(10) })), true, "a re-publish");
  assert.equal(isNewer(known, makeArtifact({ id: "a1", revision: 2, updatedAt: later(11), scope: Scope.PROJECT })), true, "a move");
  assert.equal(isNewer(known, makeArtifact({ id: "a1", revision: 2, updatedAt: later(10) })), false, "the same again");
  assert.equal(isNewer(known, makeArtifact({ id: "a1", revision: 1, updatedAt: later(30) })), false, "an older revision");
  assert.equal(isNewer(undefined, known), true, "anything is newer than nothing");
});

test("only a new artifact or a new revision is a publish; a move is not", () => {
  const list = [makeArtifact({ id: "a1", revision: 2 })];
  assert.equal(isPublish(list, makeArtifact({ id: "a9" })), true);
  assert.equal(isPublish(list, makeArtifact({ id: "a1", revision: 3 })), true);
  assert.equal(isPublish(list, makeArtifact({ id: "a1", revision: 2, updatedAt: later(5), scope: Scope.PROJECT })), false);
});

test("a change replaces the artifact where it is, without re-sorting; an unknown one is left out", () => {
  const a = makeArtifact({ id: "a", updatedAt: later(2) });
  const b = makeArtifact({ id: "b", updatedAt: later(1) });
  const moved = makeArtifact({ id: "b", updatedAt: later(9), scope: Scope.PROJECT });
  const list = applyChange([a, b], moved);
  assert.deepEqual(list.map((x) => `${x.id}:${x.scope}`), ["a:task", "b:project"]);
  assert.deepEqual(applyChange([a], moved).map((x) => x.id), ["a"]);
});

test("withoutArtifact drops one by id", () => {
  const list = [makeArtifact({ id: "a" }), makeArtifact({ id: "b" })];
  assert.deepEqual(withoutArtifact(list, "a").map((x) => x.id), ["b"]);
  assert.equal(list.length, 2, "does not mutate");
});

test("only a file-backed artifact can move to the project: a dev-server url dies with its session", () => {
  assert.equal(isPromotable(makeArtifact({ kind: "page" })), true);
  assert.equal(isPromotable(makeArtifact({ kind: "url", path: "", url: "http://localhost:5173/" })), false);
});

test("a task has an artifact it produced, or a project asset attached to it", () => {
  const a = makeArtifact({ ticketId: "t1", scope: "project", attachedTicketIds: ["t2"] });
  assert.equal(relationTo(a, "t1"), "produced");
  assert.equal(relationTo(a, "t2"), "attached");
  assert.equal(relationTo(a, "t3"), null);
  assert.equal(belongsTo(a, "t2"), true);
  assert.equal(belongsTo(a, "t3"), false);
});

test("attachable tasks leave out the producer and the tasks already attached", () => {
  const a = makeArtifact({ ticketId: "t1", scope: "project", attachedTicketIds: ["t2"] });
  const tasks = ["t1", "t2", "t3", "t4"].map((id) => ({ id, title: id, href: "" }));
  assert.deepEqual(attachableTasks(a, tasks).map((t) => t.id), ["t3", "t4"]);
});

test("attachable assets are the project assets the task does not have yet", () => {
  const mine = makeArtifact({ id: "mine", ticketId: "t1", scope: "project" });
  const onIt = makeArtifact({ id: "on", ticketId: "t2", scope: "project", attachedTicketIds: ["t1"] });
  const free = makeArtifact({ id: "free", ticketId: "t2", scope: "project" });
  assert.deepEqual(attachableAssets([mine, onIt, free], "t1").map((a) => a.id), ["free"]);
});

test("isNotOlder: a later revision or a later-or-equal change wins; an attach bumps nothing, so a tie goes to the incoming copy", () => {
  const known = makeArtifact({ revision: 2, updatedAt: later(10) });
  assert.equal(isNotOlder(undefined, known), true);
  assert.equal(isNotOlder(known, makeArtifact({ revision: 3, updatedAt: later(0) })), true);
  assert.equal(isNotOlder(known, makeArtifact({ revision: 1, updatedAt: later(99) })), false);
  assert.equal(isNotOlder(known, makeArtifact({ revision: 2, updatedAt: later(9) })), false);
  assert.equal(isNotOlder(known, makeArtifact({ revision: 2, updatedAt: later(10), attachedTicketIds: ["t2"] })), true);
});

test("applyProjectChange adds, replaces and drops by the list's keep rule, newest first", () => {
  const keep = (a) => a.scope === "project";
  const a = makeArtifact({ id: "a", scope: "project", updatedAt: later(1) });
  const b = makeArtifact({ id: "b", scope: "project", updatedAt: later(2) });
  let list = applyProjectChange([], { kind: "changed", artifact: a }, keep);
  list = applyProjectChange(list, { kind: "changed", artifact: b }, keep);
  assert.deepEqual(list.map((x) => x.id), ["b", "a"]);

  const attached = makeArtifact({ id: "a", scope: "project", updatedAt: later(1), attachedTicketIds: ["t2"] });
  list = applyProjectChange(list, { kind: "changed", artifact: attached }, keep);
  assert.deepEqual(list.map((x) => [x.id, x.attachedTicketIds.join()]), [["b", ""], ["a", "t2"]], "an attach replaces in place");

  const movedBack = makeArtifact({ id: "b", scope: "task", updatedAt: later(3) });
  list = applyProjectChange(list, { kind: "changed", artifact: movedBack }, keep);
  assert.deepEqual(list.map((x) => x.id), ["a"], "it no longer belongs: dropped");

  const stale = makeArtifact({ id: "a", scope: "task", updatedAt: later(0) });
  assert.equal(applyProjectChange(list, { kind: "changed", artifact: stale }, keep), list, "an older copy changes nothing");
  assert.deepEqual(applyProjectChange(list, { kind: "changed", artifact: movedBack }, keep), list, "not on the list and not kept: nothing");
  assert.deepEqual(applyProjectChange(list, { kind: "deleted", id: "a", projectId: "p1", ticketId: "t1", attachedTicketIds: [] }, keep), []);
});
