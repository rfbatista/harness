import { assert, file, test } from "../../../shared/testing/test.js";
import { at, makeNotification } from "../testing/fixtures.js";
import { isSettled, isUnread, markedRead, matches, merge, upsert } from "./notification.js";

file("notifications/domain/notification");

test("unread means neither read nor resolved", () => {
  assert.ok(isUnread(makeNotification()));
  assert.ok(!isUnread(makeNotification({ readAt: at(1) })));
  assert.ok(!isUnread(makeNotification({ resolvedAt: at(1) })));
  assert.ok(isSettled(makeNotification({ resolvedAt: at(1) })));
  assert.ok(!isSettled(makeNotification({ readAt: at(1) })), "read is not settled");
});

test("a filter matches by project and by unread", () => {
  const n = makeNotification({ projectId: "p2" });
  assert.ok(matches({}, n));
  assert.ok(matches({ projectId: "p2", unread: true }, n));
  assert.ok(!matches({ projectId: "p1" }, n));
  assert.ok(!matches({ unread: true }, makeNotification({ readAt: at(1) })));
});

test("upsert replaces by id and keeps newest first, ties by id", () => {
  const list = upsert([], [makeNotification({ id: "a", createdAt: at(1) }), makeNotification({ id: "b", createdAt: at(2) })]);
  assert.deepEqual(list.map((n) => n.id), ["b", "a"]);
  const next = upsert(list, [makeNotification({ id: "a", createdAt: at(1), readAt: at(3) }), makeNotification({ id: "c", createdAt: at(2) })]);
  assert.deepEqual(next.map((n) => n.id), ["c", "b", "a"]);
  assert.ok(next[2].readAt, "the newer copy wins");
});

test("a live item joins a filtered list only when it matches; a listed one is replaced even when it no longer does", () => {
  const filter = { projectId: "p1", unread: true };
  const listed = makeNotification({ id: "a", projectId: "p1", createdAt: at(1) });
  const list = merge(
    [listed],
    [
      makeNotification({ id: "b", projectId: "p1", createdAt: at(2) }),
      makeNotification({ id: "other", projectId: "p2", createdAt: at(3) }),
      makeNotification({ id: "read", projectId: "p1", createdAt: at(4), readAt: at(4) }),
    ],
    filter,
  );
  assert.deepEqual(list.map((n) => n.id), ["b", "a"]);
  const after = merge(list, [{ ...listed, readAt: at(5) }], filter);
  assert.deepEqual(after.map((n) => n.id), ["b", "a"], "a read row stays where it stands");
  assert.ok(after[1].readAt);
});

test("markedRead stamps an unread one and leaves a read or resolved one as is", () => {
  const unread = makeNotification();
  assert.equal(markedRead(unread, at(5)).readAt.getTime(), at(5).getTime());
  const read = makeNotification({ readAt: at(1) });
  assert.equal(markedRead(read, at(5)), read);
  const settled = makeNotification({ resolvedAt: at(1) });
  assert.equal(markedRead(settled, at(5)), settled);
});
