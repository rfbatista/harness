import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { isUnread, markedRead, matches, merge, upsert } from "./notification";
import { notification, read } from "../testing/fixtures";

const ids = (list: readonly { id: string }[]) => list.map((n) => n.id);

describe("notification rules", () => {
  test("unread means neither read nor resolved", () => {
    const n = notification();
    assert.deepEqual([isUnread(n), isUnread(read(n)), isUnread({ ...n, resolvedAt: new Date() })], [true, false, false]);
  });

  test("a notification is marked read once; a read one is returned as it is", () => {
    const n = notification();
    const at = new Date(Date.UTC(2026, 10, 21, 14));
    assert.equal(markedRead(n, at).readAt, at);
    const done = read(n);
    assert.equal(markedRead(done, at), done);
  });

  test("an arrival joins newest first; the same arrival twice changes nothing", () => {
    const [newer, older] = [notification(), notification()] as const; // fixtures age with each call
    const arrival = notification({ createdAt: new Date(newer.createdAt.getTime() + 60_000) });
    const once = upsert([newer, older], [arrival]);
    assert.deepEqual(ids(once), [arrival.id, newer.id, older.id]);
    assert.deepEqual(upsert(once, [arrival]), once);
  });

  test("a read or resolved change replaces the notification where it stands", () => {
    const [a, b] = [notification(), notification()] as const;
    const resolved = { ...b, resolvedAt: new Date() };
    assert.deepEqual(upsert([a, b], [resolved]), [a, resolved]);
  });

  test("a change in an Unread list keeps a row read meanwhile, and adds only unread arrivals", () => {
    const [a, b] = [notification(), notification()] as const;
    const arrivalRead = notification({ readAt: new Date() });
    const arrivalUnread = notification();
    const list = merge([a, b], [read(a), arrivalRead, arrivalUnread], { unread: true });
    assert.deepEqual(ids(list).sort(), [a.id, b.id, arrivalUnread.id].sort());
    assert.equal(list.find((n) => n.id === a.id)?.readAt !== null, true);
  });

  test("an All list takes every arrival", () => {
    assert.equal(matches({}, read(notification())), true);
    assert.equal(matches({ unread: true }, read(notification())), false);
  });

  test("the same instant breaks ties by id, so the order is stable", () => {
    const at = new Date(Date.UTC(2026, 10, 21));
    const [x, y] = [notification({ id: "a", createdAt: at }), notification({ id: "b", createdAt: at })] as const;
    assert.deepEqual(ids(upsert([x], [y])), ["b", "a"]);
  });
});
