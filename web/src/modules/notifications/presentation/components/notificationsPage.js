// The inbox page, /notifications: every notification newest first, filtered
// by project and unread (a GET form, so the server renders the first page
// and the URL can be shared), with Mark all read for the filter's scope and
// Load more. The server's rows stand in [data-ssr] until this takes over.
//
//   <main x-data="notificationsPage" data-seed="notifications-page-seed"
//         data-project-id="p1" data-project-name="harness" data-unread>

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { markedRead, merge } from "../../domain/notification.js";
import { toRow } from "../view.js";
import { pageSeed } from "./notificationsBell.js";

const PAGE_LIMIT = 50;
const TICK_MS = 30_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").NotificationGateway,
 *   store: ReturnType<typeof import("../store.js").notificationsStore>,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const notificationsPage = ({ gateway, store, clock }) => () => {
  let unwatch = () => {};
  let ticker = null;

  return {
    projectId: "",
    projectName: "",
    unreadOnly: false,
    /** @type {import("../../domain/notification.js").Notification[]} */
    items: [],
    nextBefore: "",
    live: false,
    loadingMore: false,
    marking: false,
    error: null,
    now: clock.now(),

    get rows() {
      return this.items.map((n) => toRow(n, this.now));
    },
    get hasRows() {
      return this.items.length > 0;
    },
    get isEmpty() {
      return this.live && this.items.length === 0;
    },
    get emptyText() {
      if (this.unreadOnly) return this.projectName ? `Nothing unread in ${this.projectName}.` : "Nothing unread.";
      return this.projectName ? `No notifications in ${this.projectName} yet.` : "No notifications yet.";
    },
    get hasMore() {
      return this.nextBefore !== "";
    },
    get markAllLabel() {
      return this.projectName ? `Mark all read in ${this.projectName}` : "Mark all read";
    },
    get cannotMarkAll() {
      return this.marking || !this.items.some((n) => n.readAt === null && n.resolvedAt === null);
    },
    get filter() {
      return { projectId: this.projectId, unread: this.unreadOnly };
    },

    init() {
      const el = this.$el;
      this.projectId = el.dataset.projectId ?? "";
      this.projectName = el.dataset.projectName ?? "";
      this.unreadOnly = el.dataset.unread !== undefined;
      store.start(pageSeed(el.ownerDocument));
      try {
        const page = gateway.fromSeed(readSeed(el));
        store.listed(page, store.stamp);
        this.items = page.notifications;
        this.nextBefore = page.nextBefore;
      } catch (err) {
        this.error = describeError(err);
      }
      el.querySelector("[data-ssr]")?.remove();
      this.live = true;
      unwatch = store.watch({
        apply: (changed) => (this.items = merge(this.items, changed, this.filter)),
        readAll: (projectId, at) => (this.items = this.items.map((n) => (!projectId || n.projectId === projectId ? markedRead(n, at) : n))),
        reload: () => this.reload(),
      });
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
    },

    destroy() {
      unwatch();
      clearInterval(ticker);
    },

    /** Reads the first page again: changes were missed, or a project went. */
    async reload() {
      const since = store.stamp;
      try {
        const page = await gateway.list({ ...this.filter, limit: PAGE_LIMIT });
        store.listed(page, since);
        this.items = page.notifications;
        this.nextBefore = page.nextBefore;
        this.now = clock.now();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    async loadMore() {
      if (!this.hasMore || this.loadingMore) return;
      this.loadingMore = true;
      this.error = null;
      const since = store.stamp;
      try {
        const page = await gateway.list({ ...this.filter, before: this.nextBefore, limit: PAGE_LIMIT });
        store.listed(page, since);
        this.items = [...this.items, ...page.notifications.filter((n) => !this.items.some((x) => x.id === n.id))];
        this.nextBefore = page.nextBefore;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loadingMore = false;
      }
    },

    find(event) {
      const { id } = event.currentTarget.dataset;
      return this.items.find((n) => n.id === id);
    },
    openItem(event) {
      const n = this.find(event);
      if (n) store.open(n, event);
    },
    async markOne(event) {
      const n = this.find(event);
      if (!n) return;
      this.error = null;
      try {
        await store.markRead([n]);
      } catch (err) {
        this.error = describeError(err);
      }
    },
    async markAll() {
      this.marking = true;
      this.error = null;
      try {
        await store.markAllRead(this.projectId);
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.marking = false;
      }
    },
    dismissError() {
      this.error = null;
    },
  };
};
