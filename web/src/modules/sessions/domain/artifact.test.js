import { assert, file, test } from "../../../shared/testing/test.js";
import { A0, makeArtifact } from "../testing/artifact-fixtures.js";
import { applyPublish, byUpdated, fileName, isKnown, isLoopbackUrl, KINDS } from "./artifact.js";

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
