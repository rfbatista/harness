// The top bar's bell: the unread count on every page, and a panel with the
// latest notifications across every project. Without JavaScript the bell is
// a link to the inbox page; with it, it discloses the panel (not a menu: the
// panel holds links and buttons, so Tab moves through it as anywhere else).
//
//   <div class="[ notifications ]" x-data="notificationsBell">
//     <a class="[ button ] [ bell ]" href="/notifications" x-ref="trigger" x-on:click="toggle">…<span class="[ badge ]" x-text="badge"></span></a>
//     <section class="[ popup ] [ notifications-panel ]" x-ref="panel" x-on:keydown="panelKey">…</section>
//   </div>
//
// It starts the page's store with the server's seed (#notifications-seed),
// and keeps the panel's list as one of the store's views.

import { describeError } from "../../../../shared/presentation/errors.js";
import { defaultEnv, floating } from "../../../../shared/presentation/components/popup.js";
import { markedRead, merge } from "../../domain/notification.js";
import { PANEL_LIMIT } from "../store.js";
import { badgeText, bellLabel, toRow } from "../view.js";

const SEED_ID = "notifications-seed";
const TICK_MS = 30_000;

/** The server's first paint, or null when the page has none. */
export function pageSeed(doc) {
  const el = doc.getElementById(SEED_ID);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent ?? "null");
  } catch {
    return null;
  }
}

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").NotificationGateway,
 *   store: ReturnType<typeof import("../store.js").notificationsStore>,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   env?: import("../../../../shared/presentation/components/popup.js").Env,
 * }} deps
 */
export const notificationsBell = ({ gateway, store, clock, env }) => () => {
  let popup = null;
  let unwatch = () => {};
  let ticker = null;
  let abort = null;

  return {
    /** @type {import("../../domain/notification.js").Notification[]} */
    items: [],
    unreadOnly: false,
    open: false,
    loaded: false,
    loading: false,
    error: null,
    now: clock.now(),

    // ── what the markup binds ────────────────────────────────────────────
    get badge() {
      return badgeText(store.unreadCount);
    },
    get hasUnread() {
      return !!store.unreadCount;
    },
    get label() {
      return bellLabel(store.unreadCount);
    },
    get expanded() {
      return this.open ? "true" : "false";
    },
    get rows() {
      return this.items.map((n) => toRow(n, this.now));
    },
    get isEmpty() {
      return this.loaded && this.items.length === 0;
    },
    get emptyText() {
      return this.unreadOnly ? "Nothing unread." : "No notifications yet.";
    },
    get showsAll() {
      return this.unreadOnly ? "false" : "true";
    },
    get showsUnread() {
      return this.unreadOnly ? "true" : "false";
    },
    get canMarkAll() {
      return !!store.unreadCount;
    },
    get alertsPressed() {
      return store.alerts === "on" ? "true" : "false";
    },
    get alertsUnavailable() {
      return store.alerts === "unsupported" || store.alerts === "denied";
    },
    get alertsNote() {
      if (store.alerts === "denied") return "Blocked in this browser's site settings.";
      if (store.alerts === "unsupported") return "This browser cannot show them.";
      return "";
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      store.start(pageSeed(this.$el.ownerDocument));
      const seed = store.takeSeed();
      if (seed) {
        this.items = seed.notifications.slice(0, PANEL_LIMIT);
        this.loaded = true;
      }
      unwatch = store.watch({
        apply: (changed) => this.apply(changed),
        readAll: (projectId, at) => this.readAll(projectId, at),
        reload: () => this.loaded && this.load(),
      });
      popup = floating(this.$refs.panel, {
        anchor: this.$refs.trigger,
        env: env ?? defaultEnv(),
        onDismiss: () => this.close(),
        align: () => "end",
      });
    },

    destroy() {
      unwatch();
      popup?.close();
      clearInterval(ticker);
      abort?.abort();
    },

    // ── the list, as one of the store's views ────────────────────────────
    apply(changed) {
      this.items = merge(this.items, changed, { unread: this.unreadOnly }).slice(0, PANEL_LIMIT);
    },
    readAll(projectId, at) {
      this.items = this.items.map((n) => (!projectId || n.projectId === projectId ? markedRead(n, at) : n));
    },
    async load() {
      abort?.abort();
      abort = new AbortController();
      const since = store.stamp;
      this.loading = true;
      this.error = null;
      try {
        const page = await gateway.list({ unread: this.unreadOnly, limit: PANEL_LIMIT }, abort.signal);
        store.listed(page, since);
        this.items = page.notifications;
        this.loaded = true;
        this.now = clock.now();
      } catch (err) {
        if (err?.name !== "AbortError") this.error = describeError(err);
      } finally {
        this.loading = false;
      }
    },

    // ── the panel ────────────────────────────────────────────────────────
    toggle(event) {
      event?.preventDefault();
      if (this.open) this.close({ refocus: true });
      else this.show();
    },
    show() {
      this.open = true;
      this.now = clock.now();
      popup.open({ sheet: popup.isPhone(), tall: true });
      if (!this.loaded) this.load();
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
      this.$nextTick(() => (this.$refs.panel.querySelector("[data-item]") ?? this.$refs.panel.querySelector("h2"))?.focus());
    },
    /** @param {{ refocus?: boolean }} [options] */
    close({ refocus = false } = {}) {
      if (!this.open) return;
      this.open = false;
      popup.close();
      clearInterval(ticker);
      if (refocus) this.$refs.trigger.focus();
    },
    /** Esc closes and gives the focus back; the arrows move between the items. */
    panelKey(event) {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        this.close({ refocus: true });
        return;
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const links = [...this.$refs.panel.querySelectorAll("[data-item]")];
      if (links.length === 0) return;
      event.preventDefault();
      const i = links.indexOf(this.$refs.panel.ownerDocument.activeElement);
      const next = event.key === "ArrowDown" ? (i + 1) % links.length : (i <= 0 ? links.length : i) - 1;
      links[next].focus();
    },
    /** Focus that leaves both the bell and the panel closes it. */
    focusLeft(event) {
      const to = event.relatedTarget;
      if (to instanceof Node && (this.$refs.panel.contains(to) || this.$refs.trigger.contains(to))) return;
      if (to) this.close();
    },

    showAll() {
      this.filterTo(false);
    },
    showUnread() {
      this.filterTo(true);
    },
    filterTo(unread) {
      if (this.unreadOnly === unread) return;
      this.unreadOnly = unread;
      this.load();
    },

    // ── the person's actions ─────────────────────────────────────────────
    find(event) {
      const { id } = event.currentTarget.dataset;
      return this.items.find((n) => n.id === id);
    },
    openItem(event) {
      const n = this.find(event);
      if (!n) return;
      store.open(n, event);
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
      this.error = null;
      try {
        await store.markAllRead();
      } catch (err) {
        this.error = describeError(err);
      }
    },
    toggleAlerts() {
      store.toggleAlerts();
    },
    dismissError() {
      this.error = null;
    },
  };
};
