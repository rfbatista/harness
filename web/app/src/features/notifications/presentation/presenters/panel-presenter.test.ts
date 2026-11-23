import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Inbox } from "../../application/inbox";
import { NotificationsSourceError } from "../../application/ports";
import type { Notification } from "../../domain/notification";
import { memoryNavigator } from "../../infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../../infrastructure/memory-notifications-source";
import { notification, read } from "../../testing/fixtures";
import { BellPresenter } from "./bell-presenter";
import { PanelPresenter } from "./panel-presenter";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const NOW = Date.UTC(2026, 10, 21, 12, 30);
const clock = { now: () => NOW };

function setup(notifications: readonly Notification[], { seeded = true }: { seeded?: boolean } = {}) {
  const source = memoryNotificationsSource();
  source.seed(notifications);
  const navigator = memoryNavigator();
  const inbox = new Inbox(source, { navigator, now: () => new Date(NOW), wait: () => new Promise(() => {}) });
  const page = { notifications: [...notifications].sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime()), unreadCount: source.unreadCount, nextBefore: "" };
  const panel = new PanelPresenter(inbox, source, clock, "/notifications", seeded ? page : null);
  const bell = new BellPresenter(inbox, "/notifications");
  inbox.start(seeded ? page : null);
  return { source, inbox, panel, bell, navigator };
}
const titles = (panel: PanelPresenter) => panel.viewModel.rows.map((r) => r.title);

describe("panel: opening and closing", () => {
  test("starts closed and toggles open and closed, asking for focus on the panel then the bell", () => {
    const { panel } = setup([notification()]);
    assert.equal(panel.viewModel.open, false);
    panel.toggle();
    assert.equal(panel.viewModel.open, true);
    assert.equal(panel.viewModel.focus?.target, "panel");
    panel.toggle();
    assert.equal(panel.viewModel.open, false);
    assert.equal(panel.viewModel.focus?.target, "bell");
  });

  test("Escape closes it and asks for the focus back on the bell", () => {
    const { panel } = setup([notification()]);
    panel.toggle();
    const before = panel.viewModel.focus!.id;
    panel.escape();
    assert.equal(panel.viewModel.open, false);
    assert.deepEqual([panel.viewModel.focus?.target, panel.viewModel.focus!.id > before], ["bell", true]);
  });

  test("focus leaving the bell and the panel closes it; focus moving inside does not", () => {
    const { panel } = setup([notification()]);
    panel.toggle();
    panel.focusMoved(false);
    assert.equal(panel.viewModel.open, true);
    panel.focusMoved(true);
    assert.equal(panel.viewModel.open, false);
    assert.equal(panel.viewModel.focus?.target, "panel", "the focus went where the person put it");
  });

  test("links to all notifications", () => {
    assert.equal(setup([]).panel.viewModel.inboxHref, "/notifications");
  });
});

describe("panel: the list", () => {
  test("shows the latest notifications from the props page, newest first, and loads nothing", () => {
    const [a, b] = [notification({ title: "A" }), notification({ title: "B" })] as const;
    const { panel, source } = setup([b, a]);
    assert.deepEqual(titles(panel), ["A", "B"]);
    assert.equal(source.followers, 1);
  });

  test("without a props page it loads the latest when first opened", async () => {
    const { panel } = setup([notification({ title: "A" })], { seeded: false });
    assert.deepEqual(titles(panel), []);
    panel.toggle();
    assert.equal(panel.viewModel.loading, true);
    await settle();
    assert.deepEqual(titles(panel), ["A"]);
    assert.equal(panel.viewModel.loading, false);
  });

  test("shows the row's words: level, state, age and accessible names", () => {
    const n = notification({ title: "Review wanted", level: "action", createdAt: new Date(NOW - 5 * 60_000) });
    const { panel } = setup([n]);
    assert.equal(panel.viewModel.rows[0]?.age, "", "ages are read once the view has mounted");
    panel.tick();
    const [row] = panel.viewModel.rows;
    assert.deepEqual([row?.statusText, row?.state, row?.age, row?.markLabel], ["Needs you", "unread", "5m", "Mark “Review wanted” read"]);
    assert.match(row!.label, /^Unread\. Needs you\. harness Review wanted\. harness › Ship the thing\. .*, 5m ago\.$/);
  });

  test("switches between All and Unread, which reads the list again", async () => {
    const { panel } = setup([notification({ title: "unread" }), read(notification({ title: "done" }))]);
    assert.deepEqual(titles(panel).sort(), ["done", "unread"]);
    panel.showUnread();
    await settle();
    assert.deepEqual(titles(panel), ["unread"]);
    assert.equal(panel.viewModel.unreadOnly, true);
    panel.showAll();
    await settle();
    assert.deepEqual(titles(panel).sort(), ["done", "unread"]);
  });

  test("says when there is nothing, by filter", async () => {
    const { panel } = setup([]);
    assert.deepEqual([panel.viewModel.empty, panel.viewModel.emptyText], [true, "No notifications yet."]);
    panel.showUnread();
    await settle();
    assert.equal(panel.viewModel.emptyText, "Nothing unread.");
  });

  test("a failed load shows the error with a Dismiss", async () => {
    const { panel, source } = setup([], { seeded: false });
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "The harness server is not reachable."));
    panel.toggle();
    await settle();
    assert.equal(panel.viewModel.error, "The notifications could not be loaded. The harness server did not answer.");
    panel.dismissError();
    assert.equal(panel.viewModel.error, null);
  });
});

describe("panel: marking read", () => {
  test("mark all read marks the rows at once and the server agrees; reading does not settle a cause, so the badge stays", async () => {
    const { panel, bell, source } = setup([notification(), notification()]);
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you, 2 unread"]);
    const done = panel.markAll();
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you"]);
    assert.equal(panel.viewModel.canMarkAll, false);
    assert.deepEqual(panel.viewModel.rows.map((r) => r.state), ["read", "read"]);
    await done;
    assert.equal(source.unreadCount, 0);
    assert.equal(source.openCount, 2);
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you"]);
  });

  test("a failed mark all read shows an error, keeps the count, and Dismiss clears it", async () => {
    const { panel, bell, source } = setup([notification(), notification()]);
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    await panel.markAll();
    assert.equal(panel.viewModel.error, "The notifications could not be loaded. The harness server did not answer.");
    assert.equal(bell.viewModel.badge, "2");
    source.heal();
    panel.showUnread(); // the list is read again, and shows what the server still holds unread
    await settle();
    assert.deepEqual(panel.viewModel.rows.map((r) => r.state), ["unread", "unread"]);
    panel.dismissError();
    assert.equal(panel.viewModel.error, null);
  });

  test("marking one read drops the unread count by one and leaves what needs the person alone", async () => {
    const [a, b] = [notification(), notification()] as const;
    const { panel, bell, source } = setup([a, b]);
    await panel.markOne(a.id);
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["2", "Notifications, 2 need you, 1 unread"]);
    assert.equal(source.unreadCount, 1);
    assert.equal(panel.viewModel.rows.find((r) => r.id === a.id)?.state, "read");
  });

  for (const echo of ["before", "after"] as const) {
    test(`the count is right whether the stream echoes the read ${echo} the answer`, async () => {
      const [a, b, c] = [notification(), notification(), notification()] as const;
      const { panel, bell, source } = setup([a, b, c]);
      source.echo = echo;
      await panel.markOne(a.id);
      assert.equal(bell.viewModel.badge, "3");
      assert.equal(bell.viewModel.label, "Notifications, 3 need you, 2 unread");
    });
  }

  test("a stale unread copy that arrives while a read is in flight does not bring the row back", async () => {
    const a = notification();
    const { panel, source } = setup([a]);
    const done = panel.markOne(a.id);
    source.publish({ notifications: [a], unreadCount: 1 });
    await done;
    assert.equal(panel.viewModel.rows[0]?.state, "read");
  });

  test("a read that fails puts the row back, and says so", async () => {
    const a = notification();
    const { panel, source } = setup([a]);
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    await panel.markOne(a.id);
    assert.deepEqual([panel.viewModel.rows[0]?.state, panel.viewModel.error], ["unread", "The notifications could not be loaded. The harness server did not answer."]);
  });
});

describe("panel: opening a notification", () => {
  test("marks it read and takes the person to its link", async () => {
    const a = notification({ link: "/projects/p1/tasks/t9" });
    const { panel, navigator, source } = setup([a]);
    await panel.openItem(a.id);
    assert.deepEqual(navigator.opened, ["/projects/p1/tasks/t9"]);
    assert.equal(source.unreadCount, 0);
  });

  test("a click that opens a new tab only marks it", async () => {
    const a = notification();
    const { panel, navigator, source } = setup([a]);
    await panel.openItem(a.id, true);
    assert.deepEqual(navigator.opened, []);
    assert.equal(source.unreadCount, 0);
  });
});

describe("panel: live", () => {
  test("an arrival joins the top of the list and raises the count", () => {
    const { panel, bell, source } = setup([notification({ title: "old" })]);
    const arrival = notification({ title: "new", createdAt: new Date(NOW) });
    source.publish({ notifications: [arrival], unreadCount: 2 });
    assert.deepEqual(titles(panel), ["new", "old"]);
    assert.equal(bell.viewModel.badge, "2");
  });

  test("a read arriving from another tab leaves the badge alone, and a resolution lowers it", () => {
    const [a, b] = [notification(), notification()] as const;
    const { panel, bell, source } = setup([a, b]);
    source.publish({ notifications: [read(a)], unreadCount: 1, openCount: 2 });
    assert.equal(bell.viewModel.badge, "2");
    source.publish({ notifications: [{ ...b, resolvedAt: new Date(NOW) }], unreadCount: 0, openCount: 1 });
    // Open causes lead: the one read but still open, then the settled one.
    assert.deepEqual(panel.viewModel.rows.map((r) => r.state), ["read", "settled"]);
    assert.deepEqual([bell.viewModel.badge, bell.viewModel.label], ["1", "Notifications, 1 need you"]);
  });

  test("in the Unread list an arrival that is already read stays out, and a row read meanwhile stays until the next read", async () => {
    const a = notification();
    const { panel, source } = setup([a]);
    panel.showUnread();
    await settle();
    source.publish({ notifications: [read(notification())], unreadCount: 1 });
    source.publish({ notifications: [read(a)], unreadCount: 0 });
    assert.deepEqual(panel.viewModel.rows.map((r) => r.id), [a.id]);
    assert.equal(panel.viewModel.rows[0]?.state, "read");
  });

  test("the same message twice changes nothing", () => {
    const { panel, source } = setup([]);
    const arrival = notification();
    source.publish({ notifications: [arrival], unreadCount: 1 });
    source.publish({ notifications: [arrival], unreadCount: 1 });
    assert.equal(panel.viewModel.rows.length, 1);
  });

  test("an empty change (a deleted project) and a resync read the list again", async () => {
    const a = notification();
    const { panel, source } = setup([a]);
    source.seed([]);
    source.publish({ notifications: [], unreadCount: 0 });
    await settle();
    assert.deepEqual(titles(panel), []);
    source.seed([a]);
    source.setStatus("paused");
    source.setStatus("resynced");
    await settle();
    assert.deepEqual(titles(panel), [a.title]);
  });

  test("the bell shows when the stream is down, and clears when it is back", () => {
    const { bell, source } = setup([]);
    source.setStatus("paused");
    assert.equal(bell.viewModel.feed, "paused");
    source.setStatus("resynced");
    source.setStatus("live");
    assert.equal(bell.viewModel.feed, "live");
  });

  test("the ages move on with the clock", () => {
    let now = NOW;
    const source = memoryNotificationsSource();
    const n = notification({ createdAt: new Date(NOW) });
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    const panel = new PanelPresenter(inbox, source, { now: () => now }, "/notifications", { notifications: [n], unreadCount: 1, nextBefore: "" });
    panel.tick();
    assert.equal(panel.viewModel.rows[0]?.age, "now");
    now += 2 * 60_000;
    panel.tick();
    assert.equal(panel.viewModel.rows[0]?.age, "2m");
  });
});
