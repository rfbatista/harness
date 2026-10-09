import { assert, file, test } from "../../../shared/testing/test.js";
import { makeComment, makeResource } from "../testing/resource-fixtures.js";
import { commentClaim, commentSignature, echoGuard, linkClaim, removeById, resourceSignature, uploadClaim, upsert } from "./items.js";

file("tasks/domain/items");

const ids = (list) => list.map((x) => x.id);

test("upsert replaces in place and appends what is new; the lists stay oldest first", () => {
  const a = { id: "a", n: 1 };
  const b = { id: "b", n: 1 };
  let list = upsert(upsert([], a), b);
  list = upsert(list, { id: "a", n: 2 });
  assert.deepEqual(list, [{ id: "a", n: 2 }, b]);
  assert.deepEqual(ids(removeById(list, "a")), ["b"]);
});

test("claims and feed items speak the same words", () => {
  const upload = makeResource({ title: "wireframe.png", sizeBytes: 10 });
  assert.equal(uploadClaim(" wireframe.png ", 10), resourceSignature(upload));
  const link = makeResource({ kind: "link", url: "https://example.com/a", title: "x" });
  assert.equal(linkClaim(" https://example.com/a "), resourceSignature(link));
  assert.equal(commentClaim("  Use v2.\n"), commentSignature(makeComment({ body: "Use v2." })));
});

test("echo before the answer: held, then dropped when the answer names it", () => {
  const guard = echoGuard(resourceSignature);
  const claim = guard.claim(uploadClaim("a.png", 10));
  const echo = makeResource({ id: "r9", title: "a.png", sizeBytes: 10 });
  assert.ok(guard.hold(echo), "the echo waits for the answer");
  assert.deepEqual(guard.settle(claim, "r9"), [], "the answer is the echo: nothing more to show");
  assert.equal(guard.pending, 0);
});

test("echo after the answer: there is no claim left, so it is not held (the list has its id)", () => {
  const guard = echoGuard(resourceSignature);
  const claim = guard.claim(uploadClaim("a.png", 10));
  assert.deepEqual(guard.settle(claim, "r9"), []);
  assert.ok(!guard.hold(makeResource({ id: "r9", title: "a.png", sizeBytes: 10 })));
});

test("the add failed after the server stored it: the held echo shows", () => {
  const guard = echoGuard(resourceSignature);
  const claim = guard.claim(uploadClaim("a.png", 10));
  const echo = makeResource({ id: "r9", title: "a.png", sizeBytes: 10 });
  guard.hold(echo);
  assert.deepEqual(ids(guard.settle(claim, "")), ["r9"]);
});

test("a twin from another tab is shown when the answer turns out to be another id", () => {
  const guard = echoGuard(commentSignature);
  const claim = guard.claim(commentClaim("LGTM"));
  const twin = makeComment({ id: "c-other", body: "LGTM" });
  assert.ok(guard.hold(twin));
  assert.deepEqual(ids(guard.settle(claim, "c-mine")), ["c-other"]);
});

test("something that fits no claim shows at once; one claim holds one item", () => {
  const guard = echoGuard(commentSignature);
  assert.ok(!guard.hold(makeComment({ id: "c1", body: "hello" })), "no claim");
  guard.claim(commentClaim("same"));
  assert.ok(guard.hold(makeComment({ id: "c2", body: "same" })));
  assert.ok(!guard.hold(makeComment({ id: "c3", body: "same" })), "the claim already holds one");
});

test("an edit of a held item keeps it held and up to date; a delete forgets it", () => {
  const guard = echoGuard(resourceSignature);
  const claim = guard.claim(uploadClaim("a.png", 10));
  guard.hold(makeResource({ id: "r9", title: "a.png", sizeBytes: 10 }));
  assert.ok(guard.hold(makeResource({ id: "r9", title: "renamed.png", sizeBytes: 10 })), "same id: still held");
  assert.deepEqual(guard.settle(claim, "").map((r) => r.title), ["renamed.png"]);

  const again = guard.claim(uploadClaim("b.png", 5));
  guard.hold(makeResource({ id: "r10", title: "b.png", sizeBytes: 5 }));
  assert.ok(guard.drop("r10"));
  assert.ok(!guard.drop("r10"));
  assert.deepEqual(guard.settle(again, ""), [], "deleted elsewhere: nothing to show");
});
