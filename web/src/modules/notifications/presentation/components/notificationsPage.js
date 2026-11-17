// The inbox page, /notifications: every notification newest first, filtered
// by project and unread (a GET form, so the server renders the first page
// and the URL can be shared), with Mark all read for the filter's scope and
// Load more. The server's rows stand in [data-ssr] until this takes over.
//
//   <main x-data="notificationsPage" data-seed="notifications-page-seed"
//         data-project-id="p1" data-project-name="harness" data-tab="needs|unread|all">

import { readSeed } from "../../../../shared/presentation/seed.js";
import { isOpen, markedRead, merge } from "../../domain/notification.js";
import { actorOptions, filterOf, foldRuns, groupsFor, tabCounts, tabFrom } from "../ledger.js";
import { inboxError, streamNotice, toRow, toStackRow } from "../view.js";
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
    /** "needs" | "unread" | "all": which notifications the page shows. */
    tab: "all",
    /** Only what this actor caused ("" for anyone), and only this kind ("" for every kind). */
    actor: "",
    kind: "",
    /** @type {import("../../domain/notification.js").Notification[]} */
    items: [],
    nextBefore: "",
    live: false,
    loadingMore: false,
    marking: false,
    /** Resume all interrupted is in flight: its button waits for the answer. */
    resuming: false,
    error: null,
    now: clock.now(),

    /** The Needs you group and the day groups, each with its rows. */
    get groups() {
      return groupsFor(this.items, this.tab, this.now).map((g) => ({ key: g.key, title: g.title, tone: g.tone, count: g.items.length, entries: this.entriesOf(g) }));
    },
    /**
     * A group's entries: a plain row, or a folded stack of settled repeats
     * (never the Needs you group). Alpine's x-for walks `plain` and `stacks`,
     * one of which is empty, so the markup needs no condition.
     */
    entriesOf(group) {
      if (group.key === "needs") return group.items.map((n) => ({ key: n.id, plain: [toRow(n, this.now)], stacks: [] }));
      return foldRuns(group.items, group.breakBefore).map((run) =>
        run.folded
          ? { key: `stack:${run.key}`, plain: [], stacks: [{ key: run.key, head: [toStackRow(run.items, this.now)], rows: run.items.map((n) => toRow(n, this.now)) }] }
          : { key: run.key, plain: [toRow(run.items[0], this.now)], stacks: [] },
      );
    },
    /** The who select's options: the actors seen on the page, and the selected one. */
    get actors() {
      return actorOptions(this.items, this.actor);
    },
    get counts() {
      return tabCounts({ items: this.items, openCount: store.serverOpen, unreadCount: store.unreadCount, hasMore: this.nextBefore !== "", tab: this.tab });
    },
    get needsCount() {
      return String(this.counts.needs);
    },
    get unreadCount() {
      return String(this.counts.unread);
    },
    get allCount() {
      return this.counts.all === null ? "" : String(this.counts.all);
    },
    get needsPressed() {
      return String(this.tab === "needs");
    },
    get unreadPressed() {
      return String(this.tab === "unread");
    },
    get allPressed() {
      return String(this.tab === "all");
    },
    /** "9 need you · 2 unread", beside the title. */
    get summary() {
      return `${this.counts.needs} need you · ${this.counts.unread} unread`;
    },
    get hasRows() {
      return this.groups.length > 0;
    },
    get isEmpty() {
      return this.live && this.groups.length === 0;
    },
    get emptyText() {
      if (this.tab === "needs") return this.projectName ? `Nothing needs you in ${this.projectName}.` : "Nothing needs you.";
      if (this.tab === "unread") return this.projectName ? `Nothing unread in ${this.projectName}.` : "Nothing unread.";
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
      return filterOf(this.tab, this.projectId, this.actor, this.kind);
    },

    init() {
      const el = this.$el;
      this.projectId = el.dataset.projectId ?? "";
      this.projectName = el.dataset.projectName ?? "";
      this.tab = tabFrom(el.dataset.tab) || "all";
      this.actor = el.dataset.actor ?? "";
      this.kind = el.dataset.kind ?? "";
      // The server rendered only the selected who option; the page lists everyone it has seen.
      el.querySelectorAll("select[name=actor] option[data-ssr]").forEach((o) => o.remove());
      store.start(pageSeed(el.ownerDocument));
      const page = this.actor || this.kind ? null : this.seedPage(el);
      if (page) {
        store.listed(page, store.stamp);
        // The server paints the first page whole; the Needs you tab keeps the open causes of it.
        this.items = this.tab === "needs" ? page.notifications.filter(isOpen) : page.notifications;
        this.nextBefore = page.nextBefore;
      } else {
        // Who and kind are read here, not by the server; so is a page it could not read or sent no seed for.
        this.reload();
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

    /** The server's first page, or null when there is none to read. */
    seedPage(el) {
      try {
        return gateway.fromSeed(readSeed(el));
      } catch {
        return null;
      }
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
        this.error = inboxError(err);
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
        this.error = inboxError(err);
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
        this.error = inboxError(err);
      }
    },
    async markAll() {
      this.marking = true;
      this.error = null;
      try {
        await store.markAllRead(this.projectId);
      } catch (err) {
        this.error = inboxError(err);
      } finally {
        this.marking = false;
      }
    },
    /** Acknowledge a branch finding: the server settles it, and the row follows. */
    async acknowledge(event) {
      const n = this.find(event);
      if (!n) return;
      this.error = null;
      try {
        await store.acknowledge(n);
      } catch (err) {
        this.error = inboxError(err);
      }
    },
    /** Resume all interrupted: the sessions the item lists, one by one; the item shows each one's outcome. */
    async resumeAll(event) {
      if (this.resuming) return;
      const { id } = event.currentTarget.dataset;
      this.resuming = true;
      this.error = null;
      try {
        const { notification } = await gateway.resumeInterrupted(id);
        this.items = merge(this.items, [notification], this.filter);
      } catch (err) {
        this.error = inboxError(err);
      } finally {
        this.resuming = false;
      }
    },
    /** What a dropped stream says; "" while it is live. */
    get streamNotice() {
      return streamNotice(store.status);
    },
    get hasStreamNotice() {
      return this.streamNotice !== "";
    },
    /** The banner's Reload: reads the first page again. */
    async retry() {
      this.error = null;
      await this.reload();
    },
    dismissError() {
      this.error = null;
    },
  };
};
