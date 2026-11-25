// The bell and its panel follow main's notifications ledger (ADR 0096, ADR 0097):
// the badge counts what needs the person, Needs you leads the panel, a dropped
// stream says so, and changes that stream during a read are kept. Through the
// use cases on the in-memory adapters, no React.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Inbox, PANEL_LIMIT } from "../../application/inbox";
import { NotificationsSourceError, type NotificationsSource } from "../../application/ports";
import type { Notification, NotificationPage } from "../../domain/notification";
import { memoryNavigator } from "../../infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../../infrastructure/memory-notifications-source";
import { notification, read } from "../../testing/fixtures";
import { BellPresenter } from "./bell-presenter";
import { PanelPresenter } from "./panel-presenter";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const NOW = Date.UTC(2026, 10, 21, 12, 30);
const at = (minutesAgo: number) => new Date(NOW - minutesAgo * 60_000);

function setup(source: NotificationsSource, seed: NotificationPage | null) {
  const inbox = new Inbox(source, { navigator: memoryNavigator(), now: () => new Date(NOW), wait: () => new Promise(() => {}) });
  const panel = new PanelPresenter(inbox, source, { now: () => NOW }, "/notifications", seed);
  const bell = new BellPresenter(inbox, "/notifications");
  inbox.start(seed);
  return { inbox, panel, bell };
}

/** The server's first paint of these: the latest page and the global counts. */
const paint = (source: ReturnType<typeof memoryNotificationsSource>, notifications: readonly Notification[]): NotificationPage => ({ notifications: [...notifications].sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime()), unreadCount: source.unreadCount, openCount: source.openCount, nextBefore: "" });

/** Three are unread, two are open causes (ADR 0096): the review and the wait; a settled one and a read one. */
function world() {
  const review = notification({ id: "n-review", title: "Review requested: the dashboard plan", createdAt: at(6) });
  const note = notification({ id: "n-note", title: "Release notes drafted", level: "info", createdAt: at(8) });
  const wait = notification({ id: "n-wait", title: "A session waits for approval", createdAt: at(10) });
  const failed = read(notification({ id: "n-failed", title: "A session failed", level: "info", createdAt: at(4) }));
  const done = notification({ id: "n-done", title: "Redesign sidebar is in review", createdAt: at(2), resolvedAt: at(1) });
  return [review, note, wait, failed, done];
}

describe("the bell counts what needs the person", () => {
  test("on the first paint: the badge is the open count and the name says both counts", () => {
    const source = memoryNotificationsSource();
    source.seed(world());
    const { bell } = setup(source, paint(source, world()));
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label, bell.viewModel.hasBadge], ["2", "Notifications, 2 need you, 3 unread", true]);
  });

  test("when the first paint failed, the count is read from the first GET", async () => {
    const source = memoryNotificationsSource();
    source.seed(world());
    const { bell } = setup(source, null);
    assert.equal(bell.viewModel.badge, "");
    await settle();
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you, 3 unread"]);
  });

  test("a server that sends no open count is counted by the causes this page has seen", () => {
    const source = memoryNotificationsSource();
    const items = world();
    const { bell } = setup(source, { notifications: items, unreadCount: 3, nextBefore: "" });
    assert.equal(bell.viewModel.badge, "2");
    source.publish({ notifications: [notification({ title: "New" })], unreadCount: 4 });
    assert.equal(bell.viewModel.badge, "3");
    source.publish({ notifications: [{ ...items[0]!, resolvedAt: at(0) }], unreadCount: 3 });
    assert.equal(bell.viewModel.badge, "2");
  });

  test("only open causes are counted: information and settled ones are not, read or not", () => {
    const source = memoryNotificationsSource();
    const { bell } = setup(source, { notifications: [notification({ level: "info" }), read(notification({ level: "info" })), notification({ resolvedAt: at(1) })], unreadCount: 1, openCount: 0, nextBefore: "" });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["", "Notifications, 1 unread"]);
  });

  test("a cause that settles lowers the badge and the unread count with it, as the stream says", () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { bell, panel } = setup(source, paint(source, items));
    const settled = { ...items[2]!, resolvedAt: at(0) };
    source.publish({ notifications: [settled], unreadCount: 2, openCount: 1 });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["1", "Notifications, 1 need you, 2 unread"]);
    assert.equal(panel.viewModel.rows.find((r) => r.id === "n-wait")?.state, "settled");
  });

  test("reading does not settle a cause: Mark all read leaves the badge, and the unread part of the name goes", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { bell, panel } = setup(source, paint(source, items));
    await panel.markAll();
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you"]);
  });
});

describe("the panel opens with Needs you", () => {
  test("the open causes lead, then the rest newest first", () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { panel } = setup(source, paint(source, items));
    assert.deepEqual(
      panel.viewModel.rows.map((r) => `${r.title}|${r.state}`),
      ["Review requested: the dashboard plan|unread", "A session waits for approval|unread", "Redesign sidebar is in review|settled", "A session failed|read", "Release notes drafted|unread"],
    );
  });

  test("opening reads the open causes as well, however old, and they lead the panel", async () => {
    const source = memoryNotificationsSource();
    const infos = Array.from({ length: 60 }, (_, i) => notification({ title: `Info ${i}`, level: "info", createdAt: at(i + 1) }));
    const opens = Array.from({ length: 3 }, (_, i) => notification({ title: `Waiting ${i}`, createdAt: at(100 + i) }));
    source.seed([...infos, ...opens]);
    // The first paint holds only the latest page.
    const latest = (await source.list({ limit: PANEL_LIMIT })).notifications;
    const { panel, bell } = setup(source, paint(source, latest));
    assert.equal(bell.viewModel.badge, "3", "the server counts them all");
    assert.equal(panel.viewModel.rows.some((r) => r.title.startsWith("Waiting")), false);
    panel.toggle();
    await settle();
    assert.deepEqual(panel.viewModel.rows.slice(0, 3).map((r) => r.title), ["Waiting 0", "Waiting 1", "Waiting 2"]);
    assert.equal(panel.viewModel.rows.length, PANEL_LIMIT);
  });

  test("a panel seeded from the first paint reads once on its first opening, and not again on the next", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    let reads = 0;
    const counting: NotificationsSource = { ...source, follow: source.follow, markRead: source.markRead, list: (q, s) => ((reads += 1), source.list(q, s)) };
    const { panel } = setup(counting, paint(source, items));
    panel.toggle();
    await settle();
    panel.toggle();
    panel.toggle();
    await settle();
    assert.equal(reads, 2, "the latest page and the open causes, once");
  });

  test("the Unread filter reads both again, with the open causes that are unread", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { panel } = setup(source, paint(source, items));
    panel.showUnread();
    await settle();
    assert.deepEqual(panel.viewModel.rows.map((r) => r.id), ["n-review", "n-wait", "n-note"]);
  });
});

describe("the panel keeps what streams during its read", () => {
  test("a change that arrives while the read is in flight, and that its answer predates, is merged over it", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const gates: Array<() => void> = [];
    const slow: NotificationsSource = { ...source, follow: source.follow, markRead: source.markRead, list: (q, s) => new Promise((resolve, reject) => gates.push(() => source.list(q, s).then(resolve, reject))) };
    const { panel } = setup(slow, paint(source, items));
    panel.toggle(); // asks twice: the latest page and the open causes
    assert.equal(panel.viewModel.loading, true);
    const arrival = notification({ id: "n-late", title: "Arrived during the read", createdAt: at(0) });
    source.publish({ notifications: [arrival], unreadCount: 4, openCount: 3 });
    assert.equal(panel.viewModel.rows.some((r) => r.id === "n-late"), true, "shown at once");
    for (const release of gates) release(); // the answers predate the arrival
    await settle();
    assert.equal(panel.viewModel.rows.some((r) => r.id === "n-late"), true, "and kept when the answer lands");
    assert.equal(panel.viewModel.loading, false);
  });

  test("a read that a newer one replaced leaves the busy state to the newer one", async () => {
    const source = memoryNotificationsSource();
    source.seed(world());
    const gates: Array<() => void> = [];
    const slow: NotificationsSource = { ...source, follow: source.follow, markRead: source.markRead, list: (q, s) => new Promise((resolve, reject) => gates.push(() => source.list(q, s).then(resolve, reject))) };
    const { panel } = setup(slow, null);
    panel.toggle(); // read 1: two asks (after the inbox's own first read of the count)
    panel.showUnread(); // read 2: two more
    gates[0]!();
    gates[1]!();
    gates[2]!(); // the first read answers
    await settle();
    assert.equal(panel.viewModel.loading, true, "the second read is still busy");
    gates[3]!();
    gates[4]!();
    await settle();
    assert.equal(panel.viewModel.loading, false);
  });
});

describe("a dropped stream and a failed load", () => {
  test("a dropped stream says so with a Reload, and the notice goes when the stream is back", () => {
    const source = memoryNotificationsSource();
    const { panel } = setup(source, paint(source, []));
    assert.equal(panel.viewModel.streamNotice, "");
    source.setStatus("live");
    source.setStatus("paused");
    assert.equal(panel.viewModel.streamNotice, "Live updates are paused. Reconnecting; Reload shows what is there now.");
    source.setStatus("resynced");
    source.setStatus("live");
    assert.equal(panel.viewModel.streamNotice, "");
  });

  test("Reload reads the panel again and brings what the server now holds", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { panel } = setup(source, paint(source, items));
    source.seed([...items, notification({ id: "n-more", title: "Came while paused" })]);
    panel.retry();
    await settle();
    assert.equal(panel.viewModel.rows.some((r) => r.id === "n-more"), true);
  });

  test("a failed load says it in plain words, by cause, and Reload brings the rows back", async () => {
    const source = memoryNotificationsSource();
    const items = world();
    source.seed(items);
    const { panel } = setup(source, paint(source, items));
    const cases: [NotificationsSourceError, RegExp][] = [
      [new NotificationsSourceError("NETWORK_ERROR", "ECONNREFUSED 127.0.0.1:8080"), /^The notifications could not be loaded\. The harness server did not answer\.$/],
      [new NotificationsSourceError("BAD_RESPONSE", "Unexpected notification: x"), /^The notifications could not be read\. Reload; if it stays, check the harness server log\.$/],
      [new NotificationsSourceError("UNKNOWN", "sql: 500 internal"), /^The notifications could not be loaded\.$/],
    ];
    for (const [error, words] of cases) {
      source.failWith(error);
      panel.showUnread();
      await settle();
      assert.match(panel.viewModel.error ?? "", words);
      assert.doesNotMatch(panel.viewModel.error ?? "", /ECONNREFUSED|sql|500|Unexpected/);
      source.heal();
      panel.retry();
      await settle();
      assert.equal(panel.viewModel.error, null);
      assert.ok(panel.viewModel.rows.length > 0, "Reload brought the rows back");
      panel.showAll();
      await settle();
    }
  });

  test("every kind of notification loads, and a kind the client does not know reads as a generic one", () => {
    const source = memoryNotificationsSource();
    const kinds = ["review_requested", "session_waiting", "task_review", "session_failed", "commit_check_off", "branch_request", "branch_finding", "service_request", "sessions_interrupted", "agent"] as const;
    const items = kinds.map((kind, i) => notification({ kind, title: `A ${kind}`, createdAt: at(i + 1) }));
    const { panel } = setup(source, paint(source, items));
    assert.deepEqual(panel.viewModel.rows.map((r) => r.kind).sort(), [...kinds].sort());
  });
});
