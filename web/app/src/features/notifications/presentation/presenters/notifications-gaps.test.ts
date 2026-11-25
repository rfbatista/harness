// Gap tests for the bell and the panel, from the user stories of "Spec: Web UI —
// React top bar": through the use cases on the in-memory adapters, no React.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Inbox, PANEL_LIMIT } from "../../application/inbox";
import { NotificationsSourceError, type NotificationsSource } from "../../application/ports";
import type { NotificationPage } from "../../domain/notification";
import { memoryNavigator } from "../../infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../../infrastructure/memory-notifications-source";
import { notification, read } from "../../testing/fixtures";
import { BellPresenter } from "./bell-presenter";
import { PanelPresenter } from "./panel-presenter";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const NOW = Date.UTC(2026, 10, 21, 12, 30);
const clock = { now: () => NOW };
const page = (unreadCount: number, notifications: NotificationPage["notifications"] = []): NotificationPage => ({ notifications, unreadCount, openCount: unreadCount, nextBefore: "" });

function setup(source: NotificationsSource, seed: NotificationPage | null) {
  const inbox = new Inbox(source, { navigator: memoryNavigator(), now: () => new Date(NOW), wait: () => new Promise(() => {}) });
  const panel = new PanelPresenter(inbox, source, clock, "/notifications", seed);
  const bell = new BellPresenter(inbox, "/notifications");
  inbox.start(seed);
  return { inbox, panel, bell };
}

describe("bell: the count's edges", () => {
  test("99 shows 99 and 100 shows 99+, both naming the exact count, as the stream moves the count", () => {
    const source = memoryNotificationsSource();
    const { bell } = setup(source, page(98));
    source.publish({ notifications: [notification()], unreadCount: 99, openCount: 99 });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["99", "Notifications, 99 need you, 99 unread"]);
    source.publish({ notifications: [notification()], unreadCount: 100, openCount: 100 });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["99+", "Notifications, 100 need you, 100 unread"]);
    source.publish({ notifications: [], unreadCount: 99, openCount: 99 });
    assert.equal(bell.viewModel.badge, "99");
  });

  test("one unread names itself as it is, and a count back at zero clears the badge", () => {
    const source = memoryNotificationsSource();
    const n = notification();
    const { bell } = setup(source, page(1, [n]));
    assert.equal(bell.viewModel.label, "Notifications, 1 need you, 1 unread");
    source.publish({ notifications: [{ ...read(n), resolvedAt: new Date(NOW) }], unreadCount: 0, openCount: 0 });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.hasBadge, bell.viewModel.label], ["", false, "Notifications"]);
  });

  test("a page the server could not read and a read that fails: no count, no badge, and it says so; the stream then fills the count in", async () => {
    const source = memoryNotificationsSource();
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    const { bell } = setup(source, null);
    await settle();
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label, bell.viewModel.loadFailed], ["", "Notifications", true]);
    source.publish({ notifications: [notification()], unreadCount: 4, openCount: 4 });
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.loadFailed], ["4", false]);
  });

  test("a stream that is down keeps the count it had and says so; coming back clears it", () => {
    const source = memoryNotificationsSource();
    const { bell } = setup(source, page(7));
    source.setStatus("live");
    source.setStatus("paused");
    assert.deepEqual([bell.viewModel.feed, bell.viewModel.badge], ["paused", "7"]);
    source.setStatus("resynced");
    source.setStatus("live");
    assert.equal(bell.viewModel.feed, "live");
  });

  test("there is no bell when the inbox is not served, whatever the stream says", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    const bell = new BellPresenter(inbox, null);
    inbox.start(null);
    source.publish({ notifications: [notification()], unreadCount: 1 });
    assert.equal(bell.viewModel.visible, false);
  });
});

describe("panel: the live list, gaps", () => {
  test("a resolution settles the row where it stands and lowers the count, with no read", () => {
    const source = memoryNotificationsSource();
    const n = notification();
    const { panel, bell } = setup(source, page(1, [n]));
    source.publish({ notifications: [{ ...n, resolvedAt: new Date(NOW) }], unreadCount: 0, openCount: 0 });
    assert.deepEqual([panel.viewModel.rows[0]!.state, panel.viewModel.rows[0]!.statusText, bell.viewModel.badge], ["settled", "Settled", ""]);
  });

  test("an arrival while the panel is closed is there when it opens, before the open causes are read", async () => {
    const source = memoryNotificationsSource();
    const { panel } = setup(source, page(0));
    const n = notification({ title: "Fresh" });
    source.seed([n]);
    source.publish({ notifications: [n], unreadCount: 1, openCount: 1 });
    panel.toggle();
    assert.deepEqual(
      panel.viewModel.rows.map((r) => r.title),
      ["Fresh"],
    );
    // The seed holds only the latest page: opening reads the open causes too (Needs you leads).
    assert.equal(panel.viewModel.loading, true);
    await settle();
    assert.deepEqual(panel.viewModel.rows.map((r) => r.title), ["Fresh"]);
    assert.equal(panel.viewModel.loading, false);
  });

  test("the panel keeps to its latest notifications as arrivals pile up", () => {
    const source = memoryNotificationsSource();
    const { panel } = setup(source, page(0));
    for (let i = 0; i < PANEL_LIMIT + 5; i++) source.publish({ notifications: [notification()], unreadCount: i + 1 });
    assert.equal(panel.viewModel.rows.length, PANEL_LIMIT);
  });

  test("a panel that has not read anything yet still has the count read again after a resync", async () => {
    const source = memoryNotificationsSource();
    source.seed([notification(), notification(), notification()]);
    // Server's first paint had no page: the panel has not loaded, the bell read the count itself.
    const { bell } = setup(source, null);
    await settle();
    assert.equal(bell.viewModel.badge, "3");
    // Changes while the stream was down: nobody told the page.
    source.seed([notification(), notification(), notification(), notification(), notification()]);
    source.setStatus("paused");
    source.setStatus("resynced");
    await settle();
    assert.equal(bell.viewModel.badge, "5", "a resync means changes were missed: the count must be read again");
  });
});

describe("panel: mark all read, gaps", () => {
  test("a refused mark puts the count and the rows back, shows the error, and a retry that works clears it", async () => {
    const source = memoryNotificationsSource();
    const rows = [notification(), notification()];
    source.seed(rows);
    const refusing: NotificationsSource = { ...source, follow: source.follow, list: source.list, markRead: () => Promise.reject(new NotificationsSourceError("UNKNOWN", "the server is unwell")) };
    const { panel, bell } = setup(refusing, page(2, rows));
    await panel.markAll();
    await settle();
    assert.equal(panel.viewModel.error, "The notifications could not be loaded.", "plain words, not the server's");
    assert.equal(bell.viewModel.badge, "2", "the count is back");
    assert.deepEqual(panel.viewModel.rows.map((r) => r.unread), [true, true], "the rows are back");
    assert.equal(panel.viewModel.canMarkAll, true);
    panel.dismissError();
    assert.equal(panel.viewModel.error, null);
  });

  test("with the network down, a failed mark all read does not leave the rows shown as read while the count says unread", async () => {
    const source = memoryNotificationsSource();
    const rows = [notification(), notification()];
    source.seed(rows);
    const { panel, bell } = setup(source, page(2, rows));
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    await panel.markAll();
    await settle();
    assert.equal(panel.viewModel.error, "The notifications could not be loaded. The harness server did not answer.");
    assert.equal(bell.viewModel.badge, "2");
    assert.deepEqual(panel.viewModel.rows.map((r) => r.unread), [true, true], "the server read nothing: the rows must not say read");
  });

  test("a retry after a failure marks everything and clears the error", async () => {
    const source = memoryNotificationsSource();
    const rows = [notification(), notification()];
    source.seed(rows);
    const { panel, bell } = setup(source, page(2, rows));
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    await panel.markAll();
    await settle();
    source.heal();
    await panel.markAll();
    await settle();
    assert.equal(panel.viewModel.error, null);
    assert.equal(bell.viewModel.badge, "2", "reading does not settle a cause: the two still need the person");
    assert.deepEqual(panel.viewModel.rows.map((r) => r.unread), [false, false]);
  });

  test("a failure with an answer that is not the API's still shows an error and does not throw", async () => {
    const source = memoryNotificationsSource();
    const rows = [notification()];
    source.seed(rows);
    const broken: NotificationsSource = { ...source, markRead: () => Promise.reject(new SyntaxError("Unexpected token < in JSON")), follow: source.follow, list: source.list };
    const { panel } = setup(broken, page(1, rows));
    await panel.markAll();
    assert.equal(panel.viewModel.error, "The notifications could not be loaded.", "no developer message");
  });

  test("while it is in flight the count is zero, the button is off, and a notification that arrives meanwhile is shown, unread", async () => {
    const source = memoryNotificationsSource();
    const rows = [notification()];
    source.seed(rows);
    let release = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    const slow: NotificationsSource = {
      ...source,
      follow: source.follow,
      list: source.list,
      markRead: async (request) => {
        await gate;
        return source.markRead(request);
      },
    };
    const { panel, bell } = setup(slow, page(1, rows));
    const pending = panel.markAll();
    assert.deepEqual([bell.viewModel.badge, panel.viewModel.canMarkAll], ["1", false], "the cause still needs the person; the button is off");
    const arrival = notification({ title: "Arrived meanwhile" });
    source.publish({ notifications: [arrival], unreadCount: 1 });
    assert.ok(panel.viewModel.rows.some((r) => r.title === "Arrived meanwhile" && r.unread));
    release();
    await pending;
  });
});

describe("panel: switching All and Unread under slow answers", () => {
  test("the answer to a filter the person has left does not replace the list", async () => {
    const source = memoryNotificationsSource();
    const unread = notification({ title: "Unread one" });
    const done = read(notification({ title: "Read one" }));
    source.seed([unread, done]);
    const releases: Array<() => void> = [];
    const slow: NotificationsSource = {
      ...source,
      follow: source.follow,
      markRead: source.markRead,
      list: (query, signal) =>
        new Promise((resolve, reject) => {
          // The server does not honour the abort: it answers late, as a slow network does.
          releases.push(() => source.list(query, signal).then(resolve, reject));
        }),
    };
    const { panel } = setup(slow, page(1, [unread, done]));
    panel.showUnread(); // asks for Unread
    panel.showAll(); // the person changes their mind: asks for All
    // Each read asks twice (the latest page, and the open causes).
    releases[2]!();
    releases[3]!(); // All answers first
    await settle();
    releases[0]!();
    releases[1]!(); // the stale Unread answer arrives last
    await settle();
    assert.equal(panel.viewModel.unreadOnly, false);
    assert.deepEqual(panel.viewModel.rows.map((r) => r.title).sort(), ["Read one", "Unread one"]);
    assert.equal(panel.viewModel.loading, false);
  });
});
