import { assert, file, test } from "../../../shared/testing/test.js";
import { at, makeNotification } from "../testing/fixtures.js";
import { dayLabel, defaultTab, filterOf, groupsFor, tabCounts, tabFrom } from "./ledger.js";

file("notifications/presentation/ledger");

const day = (y, m, d, h = 12) => new Date(y, m - 1, d, h, 0, 0);
const NOW = day(2026, 11, 17, 15);
const n = (id, created, fields = {}) => makeNotification({ id, createdAt: created, ...fields });

test("the default tab is Needs you while anything is open, and All otherwise", () => {
  assert.deepEqual([defaultTab(3), defaultTab(0), defaultTab(null)], ["needs", "all", "all"]);
  assert.deepEqual([tabFrom("unread"), tabFrom("nope"), tabFrom(undefined)], ["unread", "", ""]);
});

test("a tab asks the server for its own filter", () => {
  assert.deepEqual(filterOf("needs", "p1"), { projectId: "p1", unread: false, open: true });
  assert.deepEqual(filterOf("unread"), { projectId: "", unread: true, open: false });
  assert.deepEqual(filterOf("all"), { projectId: "", unread: false, open: false });
});

test("open causes pin as Needs you above the day groups, newest first inside each, empty groups absent", () => {
  const items = [
    n("a", day(2026, 11, 17, 14), { level: "info" }),
    n("b", day(2026, 11, 17, 13)),
    n("c", day(2026, 11, 17, 11), { resolvedAt: day(2026, 11, 17, 12) }),
    n("d", day(2026, 11, 16, 20), { level: "info", readAt: day(2026, 11, 16, 21) }),
    n("e", day(2026, 11, 10, 9), { resolvedAt: day(2026, 11, 10, 10) }),
    n("f", day(2026, 11, 17, 10)),
  ];
  const groups = groupsFor(items, "all", NOW);
  assert.deepEqual(groups.map((g) => [g.title, g.items.map((x) => x.id)]), [
    ["Needs you", ["b", "f"]],
    ["Today", ["a", "c"]],
    ["Yesterday", ["d"]],
    ["Tue 10 Nov", ["e"]],
  ]);
  assert.deepEqual(groupsFor([n("x", day(2026, 11, 17, 9), { level: "info" })], "all", NOW).map((g) => g.title), ["Today"]);
  assert.deepEqual(groupsFor([], "all", NOW), []);
});

test("the Needs you tab is one group; a cause that settles meanwhile stays, settled, until the list is read again", () => {
  const items = [n("a", day(2026, 11, 17, 14), { resolvedAt: day(2026, 11, 17, 14) }), n("b", day(2026, 11, 17, 13))];
  assert.deepEqual(groupsFor(items, "needs", NOW).map((g) => [g.title, g.items.map((x) => x.id)]), [["Needs you", ["a", "b"]]]);
  assert.deepEqual(groupsFor([], "needs", NOW), []);
});

test("a day reads as its local calendar day, with the year only when it is not this one", () => {
  assert.deepEqual([dayLabel(day(2026, 11, 17, 0), NOW), dayLabel(day(2026, 11, 16, 23), NOW), dayLabel(day(2025, 12, 31), NOW)], ["Today", "Yesterday", "Wed 31 Dec 2025"]);
});

test("tab counts are the server's, falling back to the items held; All counts only when every one is held", () => {
  const items = [n("a", at(1)), n("b", at(2), { level: "info", readAt: at(3) })];
  assert.deepEqual(tabCounts({ items, openCount: 5, unreadCount: 7, hasMore: true, tab: "all" }), { needs: 5, unread: 7, all: null });
  assert.deepEqual(tabCounts({ items, openCount: null, unreadCount: null, hasMore: false, tab: "all" }), { needs: 1, unread: 1, all: 2 });
  assert.equal(tabCounts({ items, openCount: null, unreadCount: null, hasMore: false, tab: "needs" }).all, null, "another tab holds a filtered list");
});
