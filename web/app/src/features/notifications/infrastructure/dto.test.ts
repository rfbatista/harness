import { test } from "node:test";
import assert from "node:assert/strict";
import { toBellProps } from "./dto";

test("no notifications part means no bell", () => {
  assert.equal(toBellProps(null), null);
  assert.equal(toBellProps({ page: null }), null);
});

test("reads the inbox href and the page of the props", () => {
  const props = toBellProps({ inbox_href: "/notifications", page: { notifications: [], unread_count: 3, next_before: "x" } });
  assert.deepEqual(props, { inboxHref: "/notifications", page: { notifications: [], unreadCount: 3, nextBefore: "x" } });
});

test("a page the server could not read, or that is unreadable, is no page", () => {
  assert.equal(toBellProps({ inbox_href: "/n", page: null })?.page, null);
  assert.equal(toBellProps({ inbox_href: "/n", page: { notifications: [{ id: "" }], unread_count: 1 } })?.page, null);
});
