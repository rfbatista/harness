import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { fixedClock } from "../../../shared/infrastructure/clock.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { memoryNotifications } from "../infrastructure/memory-gateway.js";
import { at, makeNotification } from "../testing/fixtures.js";
import { notificationsPage } from "./components/notificationsPage.js";
import { notificationsStore } from "./store.js";

file("notifications/presentation/page-flows");

// The inbox page and the store over the in-memory gateway (which obeys the
// gateway contract): filters combined with a tab and a cursor, the
// acknowledge echo, a failed load and Reload, and open versus unread.

const NOW = at(300);

/** One page of the app: a store, a gateway whose list can be made to fail, and the page component bound to it. */
function app(notifications, dataset = {}) {
  const mem = memoryNotifications({ notifications, now: () => NOW });
  const failures = [];
  const calls = [];
  const gateway = {
    ...mem.gateway,
    list(query) {
      calls.push(query);
      return failures.length ? Promise.reject(failures.shift()) : mem.gateway.list(query);
    },
  };
  const doc = { title: "Notification · harness" };
  const store = notificationsStore({
    gateway,
    alerts: { permission: () => "unsupported", request: async () => "denied", show() {}, hidden: () => false },
    prefs: { get: () => null, set() {} },
    clock: fixedClock(NOW),
    navigate() {},
    announce() {},
    doc,
    wait: () => new Promise(() => {}),
  });
  store.start(null);
  const el = { dataset, querySelectorAll: () => ({ forEach() {} }), querySelector: () => null, ownerDocument: { querySelector: () => null } };
  const page = notificationsPage({ gateway, store, clock: fixedClock(NOW) })();
  page.$el = el;
  return { mem, store, page, failures, calls, doc, gateway };
}

const click = (id) => ({ currentTarget: { dataset: { id } } });
const ids = (page) => page.items.map((n) => n.id);
const who = (name, sessionId) => ({ kind: "agent", name, mode: "operate", sessionId });
const net = () => new StructuredError(Codes.NETWORK, "Failed to fetch: boom", 0);

/** n requests, newest first: devops asks for a push, every third go-developer asks for a service, every fifth is settled. */
function flood(n) {
  const list = [];
  for (let i = 0; i < n; i++) {
    const devops = i % 3 !== 0;
    list.push(
      makeNotification({
        id: `n${String(i).padStart(3, "0")}`,
        kind: devops ? "branch_request" : "service_request",
        createdAt: at(200 - i),
        actor: devops ? who("devops", "s-ops") : who("go-developer", "s-dev"),
        verb: "wants to push",
        object: { text: "agent/x", code: true },
        resolvedAt: i % 5 === 0 ? at(250) : null,
        outcome: i % 5 === 0 ? "approved" : "",
      }),
    );
  }
  return list;
}

test("tab, who and kind combine with the cursor: every page of Needs you · devops · branch_request holds only those, once each", async () => {
  const all = flood(260);
  const want = all.filter((n) => n.resolvedAt === null && n.actor.name === "devops" && n.kind === "branch_request").map((n) => n.id);
  assert.ok(want.length > 50, "the fixture needs a second page");
  const { page, calls } = app(all, { tab: "needs", actor: "devops", kind: "branch_request" });
  page.init();
  await flush();
  await flush();
  assert.deepEqual([calls.at(-1).open, calls.at(-1).actor, calls.at(-1).kind, calls.at(-1).unread], [true, "devops", "branch_request", false]);
  assert.equal(page.items.length, 50);
  assert.equal(page.hasMore, true);
  await page.loadMore();
  while (page.hasMore) await page.loadMore();
  assert.deepEqual(ids(page), want);
  const asked = calls.filter((c) => c.before);
  assert.ok(asked.length >= 1 && asked.every((c) => c.open === true && c.actor === "devops" && c.kind === "branch_request"), "the cursor travels with the filters");
  page.destroy();
});

test("the Unread tab with a who filter asks for both, and the empty text names the tab", async () => {
  const { page, calls } = app(flood(12), { tab: "unread", actor: "nobody" });
  page.init();
  await flush();
  await flush();
  assert.deepEqual([calls.at(-1).unread, calls.at(-1).actor], [true, "nobody"]);
  assert.deepEqual([page.items.length, page.isEmpty, page.emptyText], [0, true, "Nothing unread from nobody."]);
  assert.deepEqual(page.actors.map((o) => [o.name, o.selected]), [["nobody", true]], "the URL's who stays selectable though none shows");
  page.destroy();
});

test("a stream change that does not match the filters does not join the list; one that does, joins newest first", async () => {
  const { page, mem } = app(flood(6), { tab: "all", actor: "devops" });
  page.init();
  await flush();
  await flush();
  const before = ids(page);
  mem.notify({ id: "other", actor: who("go-developer", "s-dev"), createdAt: at(280) });
  assert.deepEqual(ids(page), before);
  mem.notify({ id: "mine", actor: who("devops", "s-ops"), createdAt: at(281) });
  assert.equal(ids(page)[0], "mine");
  page.destroy();
});

test("a Needs you list keeps a cause that settles meanwhile, settled, and the open count falls", async () => {
  const { page, mem, store } = app([makeNotification({ id: "a", createdAt: at(10) }), makeNotification({ id: "b", createdAt: at(20) })], { tab: "needs" });
  page.init();
  await flush();
  await flush();
  assert.equal(store.badgeCount, 2);
  mem.resolve("a");
  assert.deepEqual([store.badgeCount, ids(page), page.items.find((n) => n.id === "a").resolvedAt !== null], [1, ["b", "a"], true]);
  assert.equal(page.counts.needs, 1);
  page.destroy();
});

// ── acknowledge ───────────────────────────────────────────────────────────

const finding = (id, m = 10, over = {}) =>
  makeNotification({ id, kind: "branch_finding", createdAt: at(m), actor: { kind: "system", name: "branch watch", mode: "", sessionId: "" }, verb: "found a push with no approved request on", object: { text: "agent/x", code: true }, ...over });

test("acknowledge: the stream's echo comes before the answer; the row settles once, the count follows the stream", async () => {
  const wait = makeNotification({ id: "w", kind: "session_waiting", createdAt: at(5) });
  const { page, store } = app([finding("f", 10), wait], { tab: "all" });
  page.init();
  await flush();
  await flush();
  assert.equal(store.badgeCount, 2);
  await page.acknowledge(click("f"));
  const rows = page.items.filter((n) => n.id === "f");
  assert.equal(rows.length, 1, "the echo and the answer are one row");
  assert.deepEqual([rows[0].outcome, rows[0].resolvedAt !== null, rows[0].readAt !== null], ["acknowledged", true, true], "settled and marked read");
  assert.deepEqual([store.badgeCount, page.error], [1, null], "the other open cause stays counted");
  assert.deepEqual(ids(page), ["f", "w"], "no duplicate, no reorder");
  page.destroy();
});

test("acknowledging again returns the settled one unchanged and moves no count", async () => {
  const { page, store } = app([finding("f")], { tab: "all" });
  page.init();
  await flush();
  await flush();
  await page.acknowledge(click("f"));
  const first = page.items[0];
  await page.acknowledge(click("f"));
  assert.deepEqual([page.items.length, page.items[0].resolvedAt.getTime(), store.badgeCount, page.error], [1, first.resolvedAt.getTime(), 0, null]);
  page.destroy();
});

test("a failed acknowledge says so in plain words, leaves the cause open and counted, and Reload clears the notice", async () => {
  const { page, gateway, store } = app([finding("f")], { tab: "all" });
  page.init();
  await flush();
  await flush();
  const real = gateway.acknowledge;
  gateway.acknowledge = () => Promise.reject(net());
  await page.acknowledge(click("f"));
  assert.ok(page.error && page.error.message.length > 0 && !/NETWORK|boom|Failed to fetch/.test(page.error.message), "plain words");
  assert.deepEqual([page.items[0].resolvedAt, store.badgeCount], [null, 1]);
  gateway.acknowledge = real;
  await page.retry();
  assert.equal(page.error, null);
  page.destroy();
});

test("acknowledge on a row the page no longer holds does nothing", async () => {
  const { page } = app([finding("f")], { tab: "all" });
  page.init();
  await flush();
  await flush();
  await page.acknowledge(click("ghost"));
  assert.deepEqual([page.error, page.items[0].resolvedAt], [null, null]);
  page.destroy();
});

test("only a branch finding can be acknowledged: the gateway refuses another kind with INVALID_INPUT", async () => {
  const { gateway } = app([makeNotification({ id: "r" })]);
  await assert.rejects(gateway.acknowledge("r"), Codes.INVALID_INPUT);
  await assert.rejects(gateway.acknowledge("missing"), Codes.NOTIFICATION_NOT_FOUND);
});

// ── a failed load and Reload ──────────────────────────────────────────────

test("a failed first load says what happened in plain words; Reload reads it again and clears the notice", async () => {
  const { page, failures } = app([makeNotification({ id: "a" })], { tab: "all", actor: "harness" });
  failures.push(net());
  page.init();
  await flush();
  await flush();
  assert.ok(page.error && !/NETWORK|boom|Failed to fetch|BAD_RESPONSE/.test(page.error.message));
  assert.equal(page.items.length, 0);
  assert.equal(page.isEmpty, true, "the page does not claim a list it could not read");
  await page.retry();
  assert.deepEqual([page.error, ids(page)], [null, ["a"]]);
  page.destroy();
});

test("a failed bad-response load reads differently from a network one, and neither leaks the developer message", async () => {
  const { page, failures } = app([], { tab: "all", kind: "agent" });
  failures.push(new StructuredError(Codes.BAD_RESPONSE, "Unexpected notification: unknown level", 200));
  page.init();
  await flush();
  await flush();
  const bad = page.error.message;
  await page.retry();
  failures.push(net());
  await page.retry();
  assert.equal(bad === page.error.message, false);
  assert.equal(/Unexpected|level/.test(bad), false);
  page.destroy();
});

test("Load more that fails keeps what is shown and the cursor; Reload then recovers the first page", async () => {
  const { page, failures } = app(flood(120), { tab: "all", actor: "devops" });
  page.init();
  await flush();
  await flush();
  const held = ids(page);
  const cursor = page.nextBefore;
  assert.ok(held.length === 50 && cursor !== "");
  failures.push(net());
  await page.loadMore();
  assert.deepEqual([ids(page), page.nextBefore, page.loadingMore, page.error !== null], [held, cursor, false, true]);
  await page.loadMore();
  assert.deepEqual([page.error, page.items.length > 50], [null, true], "a later Load more starts clean");
  await page.retry();
  assert.equal(page.items.length, 50);
  page.destroy();
});

test("a resync with the stream down reloads; a dropped stream says so and a live one says nothing", async () => {
  const { page, mem, store } = app([makeNotification({ id: "a" })], { tab: "all" });
  page.init();
  await flush();
  await flush();
  assert.equal(page.hasStreamNotice, false);
  mem.reportStatus("paused");
  assert.equal(store.status, "paused");
  assert.equal(page.hasStreamNotice, true);
  assert.ok(/Reload/.test(page.streamNotice));
  mem.notify({ id: "b" });
  mem.reportStatus("resynced");
  await flush();
  await flush();
  assert.ok(ids(page).includes("b"));
  page.destroy();
});

// ── open is not unread ────────────────────────────────────────────────────

test("open ≠ unread: one open cause and three unread notes badge as 1, and reading everything leaves 1", async () => {
  const items = [makeNotification({ id: "o", createdAt: at(5) }), ...["a", "b", "c"].map((id, i) => makeNotification({ id, kind: "agent", level: "info", source: "agent", createdAt: at(10 + i) }))];
  const { page, store, doc } = app(items, { tab: "all" });
  page.init();
  await flush();
  await flush();
  assert.deepEqual([store.badgeCount, store.unreadCount, doc.title], [1, 4, "(1) Notification · harness"]);
  assert.deepEqual([page.counts.needs, page.counts.unread, page.summary], [1, 4, "1 need you · 4 unread"]);
  await page.markAll();
  assert.deepEqual([store.badgeCount, store.unreadCount, doc.title], [1, 0, "(1) Notification · harness"], "read, still needs you");
  assert.equal(page.summary, "1 need you · 0 unread");
  page.destroy();
});

test("open ≠ unread: three unread notes and no open cause show no badge and no count in the title", async () => {
  const items = ["a", "b", "c"].map((id, i) => makeNotification({ id, kind: "agent", level: "info", source: "agent", createdAt: at(10 + i) }));
  const { page, store, doc } = app(items, { tab: "all" });
  page.init();
  await flush();
  await flush();
  assert.deepEqual([store.badgeCount, store.unreadCount, doc.title], [0, 3, "Notification · harness"]);
  page.destroy();
});

test("a read open cause is still open: the Needs you tab keeps it, and Unread does not list it", async () => {
  const { page, store } = app([makeNotification({ id: "o", readAt: at(1) })], { tab: "needs" });
  page.init();
  await flush();
  await flush();
  assert.deepEqual([ids(page), page.counts.needs, page.counts.unread, store.badgeCount], [["o"], 1, 0, 1]);
  page.destroy();
});

test("an empty page says why: the who and kind filters are named, not just the tab", () => {
  const { page } = app([]);
  page.tab = "all";
  assert.equal(page.emptyText, "No notifications yet.");
  page.actor = "devops";
  assert.equal(page.emptyText, "Nothing from devops.");
  page.kind = "branch_request";
  assert.equal(page.emptyText, "Nothing from devops of that kind.");
  page.actor = "";
  assert.equal(page.emptyText, "Nothing of that kind.");
  page.tab = "needs";
  assert.equal(page.emptyText, "Nothing of that kind needs you.");
});
