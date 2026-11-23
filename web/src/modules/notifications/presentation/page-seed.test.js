import { assert, file, test } from "../../../shared/testing/test.js";
import { pageSeed } from "./page-seed.js";

file("notifications/presentation/page-seed");

const doc = (text) => ({ querySelector: (sel) => (sel === 'script[data-island-props="top-bar"]' && text !== null ? { textContent: text } : null) });

test("the seed is the page the top bar island carries", () => {
  const page = { notifications: [], unread_count: 3 };
  assert.deepEqual(pageSeed(doc(JSON.stringify({ notifications: { inbox_href: "/notifications", page } }))), page);
});

test("no seed when the island has no page, no bell, no props or unreadable props", () => {
  assert.equal(pageSeed(doc(JSON.stringify({ notifications: { page: null } }))), null);
  assert.equal(pageSeed(doc(JSON.stringify({ notifications: null }))), null);
  assert.equal(pageSeed(doc(null)), null);
  assert.equal(pageSeed(doc("{nope")), null);
});
