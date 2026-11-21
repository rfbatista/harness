import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Inbox } from "../../application/inbox";
import { NotificationsSourceError } from "../../application/ports";
import type { NotificationPage } from "../../domain/notification";
import { memoryNavigator } from "../../infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../../infrastructure/memory-notifications-source";
import { notification } from "../../testing/fixtures";
import { BellPresenter } from "./bell-presenter";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const page = (unreadCount: number): NotificationPage => ({ notifications: [], unreadCount, nextBefore: "" });

describe("bell", () => {
  test("shows the count of the props page, in the badge and in the name", () => {
    const inbox = new Inbox(memoryNotificationsSource(), { navigator: memoryNavigator() });
    inbox.start(page(3));
    const { viewModel } = new BellPresenter(inbox, "/notifications");
    assert.equal(viewModel.visible, true);
    assert.equal(viewModel.href, "/notifications");
    assert.equal(viewModel.badge, "3");
    assert.equal(viewModel.hasUnread, true);
    assert.equal(viewModel.label, "Notifications, 3 unread");
  });

  test("shows 99+ past 99 and names the exact count", () => {
    const inbox = new Inbox(memoryNotificationsSource(), { navigator: memoryNavigator() });
    inbox.start(page(120));
    const { viewModel } = new BellPresenter(inbox, "/notifications");
    assert.equal(viewModel.badge, "99+");
    assert.equal(viewModel.label, "Notifications, 120 unread");
  });

  test("shows no badge at zero", () => {
    const inbox = new Inbox(memoryNotificationsSource(), { navigator: memoryNavigator() });
    inbox.start(page(0));
    const { viewModel } = new BellPresenter(inbox, "/notifications");
    assert.equal(viewModel.badge, "");
    assert.equal(viewModel.hasUnread, false);
    assert.equal(viewModel.label, "Notifications");
  });

  test("reads the count from the source when the props carry no page", async () => {
    const source = memoryNotificationsSource();
    source.seed([notification(), notification(), notification()]);
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    const bell = new BellPresenter(inbox, "/notifications");
    inbox.start(null);
    assert.equal(bell.viewModel.badge, "");
    await settle();
    assert.equal(bell.viewModel.badge, "3");
    assert.equal(bell.viewModel.loadFailed, false);
  });

  test("is absent when the inbox is not served", () => {
    const inbox = new Inbox(memoryNotificationsSource(), { navigator: memoryNavigator() });
    inbox.start(page(5));
    assert.equal(new BellPresenter(inbox, null).viewModel.visible, false);
  });

  test("a failed read leaves the bell without a count and says so, and does not throw", async () => {
    const source = memoryNotificationsSource();
    source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline"));
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    const bell = new BellPresenter(inbox, "/notifications");
    inbox.start(null);
    await settle();
    assert.equal(bell.viewModel.loadFailed, true);
    assert.equal(bell.viewModel.badge, "");
    assert.equal(bell.viewModel.label, "Notifications");
  });

  test("starts once per page", async () => {
    const source = memoryNotificationsSource();
    source.seed([notification()]);
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(7));
    inbox.start(null);
    await settle();
    assert.equal(new BellPresenter(inbox, "/notifications").viewModel.badge, "7");
  });
});
