import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { fixedClock } from "../../../shared/infrastructure/clock.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toPage } from "../infrastructure/dto.js";
import { at, makeNotification, notificationDTO } from "../testing/fixtures.js";
import { notificationsStore } from "./store.js";

file("notifications/presentation/store");

/** A gateway the test drives: the stream, and each read's answer. */
function scripted() {
  const reads = [];
  const lists = [];
  let onChange = () => {};
  let onStatus = () => {};
  return {
    reads,
    lists,
    gateway: {
      fromSeed: toPage,
      list(query) {
        return new Promise((resolve, reject) => lists.push({ query, resolve, reject }));
      },
      markRead(request) {
        return new Promise((resolve, reject) => reads.push({ request, resolve, reject }));
      },
      follow(change, status) {
        onChange = change;
        onStatus = status;
        return () => {};
      },
    },
    /** The server streams a change. */
    push: (notifications, unreadCount) => onChange({ notifications, unreadCount }),
    status: (s) => onStatus(s),
  };
}

function world({ seed = null, permission = "default", hidden = false, pref = null } = {}) {
  const server = scripted();
  const said = [];
  const shown = [];
  const asked = [];
  const went = [];
  const stored = new Map(pref ? [["notifications.desktop", pref]] : []);
  const doc = { title: "Notification · harness" };
  const alerts = {
    perm: permission,
    permission() {
      return this.perm;
    },
    async request() {
      asked.push(true);
      return this.perm === "default" ? "granted" : this.perm;
    },
    show: (a) => shown.push(a),
    hidden: () => hidden,
  };
  const store = notificationsStore({
    gateway: server.gateway,
    alerts,
    prefs: { get: (k) => stored.get(k) ?? null, set: (k, v) => (v === null ? stored.delete(k) : stored.set(k, v)) },
    clock: fixedClock(at(10)),
    navigate: (url) => went.push(url),
    announce: (t) => said.push(t),
    doc,
    wait: () => new Promise(() => {}), // opening waits for the read, never for the clock
  });
  const view = { applied: [], readAll: [], reloads: 0 };
  store.watch({
    apply: (changed) => view.applied.push(changed.map((n) => [n.id, n.readAt === null && n.resolvedAt === null ? "unread" : "read"])),
    readAll: (projectId) => view.readAll.push(projectId),
    reload: () => (view.reloads += 1),
  });
  store.start(seed);
  return { server, store, said, shown, asked, went, doc, view, stored };
}

const seedOf = (notifications, unread_count) => ({ notifications: notifications.map(notificationDTO), unread_count });
const read = (n) => ({ ...n, readAt: at(11) });

test("the seed gives the count and the title; without one, the first read does", async () => {
  const seeded = world({ seed: seedOf([makeNotification()], 3) });
  assert.deepEqual([seeded.store.unreadCount, seeded.doc.title, seeded.server.lists.length], [3, "(3) Notification · harness", 0]);

  const bare = world();
  assert.equal(bare.store.unreadCount, null, "unknown until read");
  bare.server.lists[0].resolve({ notifications: [], unreadCount: 2, nextBefore: "" });
  await flush();
  assert.deepEqual([bare.store.unreadCount, bare.doc.title], [2, "(2) Notification · harness"]);
});

test("a new action item is counted and announced once; info, read and known ones are not", () => {
  const w = world({ seed: seedOf([makeNotification({ id: "known" })], 1) });
  w.server.push([makeNotification({ id: "a", title: "Review requested", projectName: "tui" })], 2);
  w.server.push([makeNotification({ id: "a", title: "Review requested", projectName: "tui" })], 2);
  w.server.push([makeNotification({ id: "i", level: "info" })], 3);
  w.server.push([makeNotification({ id: "known" })], 3);
  w.server.push([makeNotification({ id: "r", readAt: at(1) })], 3);
  assert.deepEqual(w.said, ["New in tui: Review requested"]);
  assert.deepEqual([w.store.unreadCount, w.doc.title], [3, "(3) Notification · harness"]);
  assert.equal(w.view.applied.length, 5, "every change reaches the views");
});

test("marking read drops the count at once; the echo before the answer neither flickers nor counts twice", async () => {
  const n = makeNotification({ id: "a" });
  const w = world({ seed: seedOf([n, makeNotification({ id: "b" })], 2) });
  const done = w.store.markRead([n]);
  assert.equal(w.store.unreadCount, 1, "optimistic");
  // A change committed before the read still carries a's unread copy and the old count.
  w.server.push([makeNotification({ id: "c", level: "info" })], 3);
  assert.equal(w.store.unreadCount, 2, "c is counted, a stays off");
  w.server.push([n, makeNotification({ id: "e", level: "info" })], 4); // a's unread copy, before the read committed
  assert.equal(w.store.unreadCount, 3);
  assert.ok(!w.view.applied.flat().some(([id, s]) => id === "a" && s === "unread"), "a's unread copy never reaches a view");
  // The echo, then a later item, then the answer (with the count as of the read).
  w.server.push([read(n)], 3);
  assert.equal(w.store.unreadCount, 3, "the echo: a is off the server's count now");
  w.server.push([makeNotification({ id: "d" })], 4);
  w.server.reads[0].resolve({ notifications: [read(n)], unreadCount: 3 });
  await done;
  assert.equal(w.store.unreadCount, 4, "after the echo the stream's later count wins");
  assert.deepEqual(w.server.reads[0].request, { ids: ["a"] });
});

test("an answer before its echo sets the count; messages committed before the read do not undo it", async () => {
  const n = makeNotification({ id: "a" });
  const w = world({ seed: seedOf([n], 1) });
  const done = w.store.markRead([n]);
  w.server.push([makeNotification({ id: "x", level: "info" })], 2); // committed before the read
  w.server.reads[0].resolve({ notifications: [read(n)], unreadCount: 1 });
  await done;
  assert.equal(w.store.unreadCount, 1);
  w.server.push([read(n)], 1); // the echo, late
  assert.equal(w.store.unreadCount, 1);
});

test("a failed read puts the rows and the count back; an unknown id reloads the views", async () => {
  const n = makeNotification({ id: "a" });
  const w = world({ seed: seedOf([n], 1) });
  const done = w.store.markRead([n]);
  w.server.reads[0].reject(new StructuredError(Codes.NOTIFICATION_NOT_FOUND, "gone", 404));
  await assert.rejects(done, Codes.NOTIFICATION_NOT_FOUND);
  assert.deepEqual(w.view.applied, [[["a", "read"]], [["a", "unread"]]]);
  assert.deepEqual([w.store.unreadCount, w.view.reloads], [1, 1]);
});

test("reading an already read one does nothing", async () => {
  const w = world({ seed: seedOf([], 0) });
  await w.store.markRead([makeNotification({ readAt: at(1) }), makeNotification({ id: "s", resolvedAt: at(1) })]);
  assert.equal(w.server.reads.length, 0);
});

test("Mark all read: every view marks its rows, the count shows 0 while it runs; a project's leaves the count to the server", async () => {
  const w = world({ seed: seedOf([makeNotification()], 4) });
  const all = w.store.markAllRead();
  assert.deepEqual([w.view.readAll, w.store.unreadCount, w.server.reads[0].request], [[""], 0, { all: true }]);
  w.server.reads[0].resolve({ notifications: [read(makeNotification())], unreadCount: 0 });
  await all;

  const p = w.store.markAllRead("p2");
  assert.deepEqual([w.view.readAll, w.server.reads[1].request], [["", "p2"], { all: true, projectId: "p2" }]);
  w.server.push([makeNotification({ id: "z" })], 1);
  w.server.reads[1].resolve({ notifications: [], unreadCount: 1 });
  await p;
  assert.equal(w.store.unreadCount, 1);
});

test("an empty change (a project deleted) and a resync reload the views; the count follows the stream", () => {
  const w = world({ seed: seedOf([], 5) });
  w.server.push([], 2);
  assert.deepEqual([w.store.unreadCount, w.view.reloads], [2, 1]);
  w.server.status(FeedStatus.PAUSED);
  w.server.status(FeedStatus.RESYNCED);
  assert.equal(w.view.reloads, 2);
});

test("a list's count is taken only when no stream message came while it was read", () => {
  const w = world({ seed: seedOf([], 1) });
  const since = w.store.stamp;
  w.server.push([makeNotification({ id: "n" })], 2);
  w.store.listed({ notifications: [], unreadCount: 1, nextBefore: "" }, since);
  assert.equal(w.store.unreadCount, 2);
  w.store.listed({ notifications: [], unreadCount: 7, nextBefore: "" }, w.store.stamp);
  assert.equal(w.store.unreadCount, 7);
});

test("opening a notification marks it read, then goes to its link; a modified click only marks it", async () => {
  const n = makeNotification({ id: "a", link: "/projects/p1/tasks/t1" });
  const w = world({ seed: seedOf([n], 1) });
  let prevented = 0;
  const click = (extra = {}) => ({ button: 0, preventDefault: () => (prevented += 1), ...extra });
  const opening = w.store.open(n, click());
  assert.deepEqual([w.went, prevented], [[], 1], "waits for the read");
  w.server.reads[0].resolve({ notifications: [read(n)], unreadCount: 0 });
  await opening;
  assert.deepEqual(w.went, ["/projects/p1/tasks/t1"]);

  const m = makeNotification({ id: "b" });
  w.server.push([m], 1);
  await w.store.open(m, click({ metaKey: true }));
  assert.deepEqual([w.went.length, prevented, w.server.reads.length], [1, 1, 2], "the browser opens the tab; the read still goes");
});

test("desktop alerts: asked for only from the toggle, remembered, and raised only while the page is hidden", async () => {
  const w = world({ hidden: true, seed: seedOf([], 0) });
  assert.deepEqual([w.store.alerts, w.asked.length], ["off", 0]);
  w.server.push([makeNotification({ id: "a" })], 1);
  assert.equal(w.shown.length, 0, "off by default");
  await w.store.toggleAlerts();
  assert.deepEqual([w.store.alerts, w.asked.length, w.stored.get("notifications.desktop")], ["on", 1, "on"]);
  w.server.push([makeNotification({ id: "b", title: "Session waiting" })], 2);
  assert.deepEqual(w.shown.map((a) => [a.title, a.tag]), [["Session waiting", "b"]]);
  await w.store.toggleAlerts();
  assert.deepEqual([w.store.alerts, w.stored.has("notifications.desktop")], ["off", false]);

  const visible = world({ hidden: false, permission: "granted", pref: "on", seed: seedOf([], 0) });
  assert.equal(visible.store.alerts, "on", "remembered");
  visible.server.push([makeNotification({ id: "c" })], 1);
  assert.equal(visible.shown.length, 0, "not while the page is in sight");

  const denied = world({ permission: "denied", seed: seedOf([], 0) });
  assert.equal(denied.store.alerts, "denied");
});
