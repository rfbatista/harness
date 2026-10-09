// The page's one model of the inbox: the global unread count, which
// notifications it has seen, and the reads in flight. The bell, the panel and
// the inbox page bind to it, and it follows the notifications stream once per
// page. Registered as Alpine.store("notifications").
//
// The count and the echo. Marking read is optimistic: the rows change and the
// count drops at once. The stream echoes the read, possibly before the POST
// answers, and messages committed before the read can still arrive carrying
// the old count and the unread copy. Until the server is seen to have read an
// id (its echo, or the answer), the id stays pending: its unread copies are
// ignored and it stays off the count. When the POST answers, its count wins
// unless the echo came first: the stream is in commit order, so after the
// echo the stream's count is the later one.

import { codeOf, Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { isUnread, markedRead } from "../domain/notification.js";

/** How long opening a notification waits for its read before it navigates. */
export const OPEN_WAIT_MS = 1_500;
/** The latest notifications the top bar's panel shows. */
export const PANEL_LIMIT = 20;

const ALERTS_KEY = "notifications.desktop";

/**
 * A view of some notifications (the panel, the inbox page) the store keeps
 * up to date.
 * @typedef {object} View
 * @property {(changed: import("../domain/notification.js").Notification[]) => void} apply
 * @property {(projectId: string, at: Date) => void} readAll  marks its rows (of projectId, or all) read
 * @property {() => void} reload  what it shows may be stale: read it again
 */

/**
 * @param {{
 *   gateway: import("../domain/ports.js").NotificationGateway,
 *   alerts: import("../domain/ports.js").DesktopAlerts,
 *   prefs: import("../../../shared/infrastructure/storage.js").Preferences,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 *   navigate: (url: string) => void,
 *   announce: (text: string) => void,
 *   doc?: { title: string },
 *   wait?: (ms: number) => Promise<void>,
 * }} deps
 */
export function notificationsStore({ gateway, alerts, prefs, clock, navigate, announce, doc = globalThis.document, wait = (ms) => new Promise((r) => setTimeout(r, ms)) }) {
  /** @type {Set<View>} */
  const views = new Set();
  /** Ids this page has shown or heard of: only the others are news. */
  const seen = new Set();
  /** Ids read here that the server is not yet seen to have read. */
  const pendingRead = new Set();
  /** Projects ("" for every one) with a Mark all read in flight. */
  const pendingAll = new Map();
  /** The reads in flight, each with the ids the stream showed read meanwhile. */
  const requests = new Set();
  /** The seed's page, for the first view that wants it. */
  let seedPage = null;
  let started = false;
  let baseTitle = "";

  const each = (fn) => {
    for (const v of [...views]) fn(v);
  };

  // Whether a copy from the server is one the person has already read here.
  const stale = (n) => isUnread(n) && (pendingRead.has(n.id) || (seen.has(n.id) && (pendingAll.has("") || pendingAll.has(n.projectId))));

  return {
    /** The server's count as last heard; null until heard. */
    serverCount: /** @type {number | null} */ (null),
    /** Reads in flight off the count: mirrors pendingRead, which Alpine does not watch. */
    pending: 0,
    allPending: false,
    status: FeedStatus.CONNECTING,
    /** @type {"off" | "on" | "denied" | "unsupported"} */
    alerts: "off",

    /** The unread count to show; null while unknown. */
    get unreadCount() {
      if (this.serverCount === null) return null;
      if (this.allPending) return 0;
      return Math.max(0, this.serverCount - this.pending);
    },

    /**
     * Starts following, once per page.
     * @param {import("../domain/notification.js").Page | null} seed  the server's first paint, or null when it had none
     */
    start(seed) {
      if (started) return;
      started = true;
      baseTitle = (doc?.title ?? "").replace(/^\(\d+\+?\) /, "");
      const permission = alerts.permission();
      if (permission === "unsupported") this.alerts = "unsupported";
      else if (prefs.get(ALERTS_KEY) === "on" && permission === "granted") this.alerts = "on";
      else if (permission === "denied") this.alerts = "denied";

      if (seed) {
        seedPage = seed;
        for (const n of seed.notifications) seen.add(n.id);
        this.serverCount = seed.unreadCount;
        this.syncTitle();
      } else {
        const since = this.stamp;
        gateway.list({ limit: PANEL_LIMIT }).then(
          (page) => {
            if (!seedPage) seedPage = page;
            this.listed(page, since);
          },
          () => {}, // the count stays unknown; the stream fills it in
        );
      }
      gateway.follow(
        (change) => this.heard(change),
        (status) => this.statusChanged(status),
      );
    },

    /** Counts stream messages, so an answer can tell whether one came meanwhile. */
    stamp: 0,

    /** The seed's page once; null after (or when there was none). */
    takeSeed() {
      const page = seedPage;
      seedPage = null;
      return page;
    },

    /** @param {View} view @returns {() => void} */
    watch(view) {
      views.add(view);
      return () => views.delete(view);
    },

    /**
     * A view read a page. Its count is the server's unless a stream message
     * came while it was asked for (`since` is the stamp when it asked).
     */
    listed(page, since) {
      for (const n of page.notifications) seen.add(n.id);
      if (this.stamp === since) {
        this.serverCount = page.unreadCount;
        this.syncTitle();
      }
    },

    /** One stream message. */
    heard(change) {
      this.stamp += 1;
      const fresh = [];
      for (const n of change.notifications) {
        if (!isUnread(n)) {
          pendingRead.delete(n.id);
          for (const r of requests) r.seenRead.add(n.id);
        } else if (!seen.has(n.id) && n.level === "action") {
          fresh.push(n);
        }
        seen.add(n.id);
      }
      this.pending = pendingRead.size;
      this.serverCount = change.unreadCount;
      this.syncTitle();

      const changed = change.notifications.filter((n) => !stale(n));
      if (changed.length) each((v) => v.apply(changed));
      // An empty change: a deleted project took its notifications along.
      if (change.notifications.length === 0) each((v) => v.reload());
      if (fresh.length) this.tell(fresh);
    },

    statusChanged(status) {
      this.status = status;
      if (status !== FeedStatus.RESYNCED) return;
      // Changes were missed while the stream was down.
      if (views.size) each((v) => v.reload());
      else {
        const since = this.stamp;
        gateway.list({ limit: 1 }).then((page) => this.listed({ ...page, notifications: [] }, since), () => {});
      }
    },

    /** Says what is new, and raises a desktop alert while the page is out of sight. */
    tell(fresh) {
      const [first] = fresh;
      announce(fresh.length === 1 ? `New in ${first.projectName}: ${first.title}` : `${fresh.length} new notifications need you`);
      if (this.alerts !== "on" || !alerts.hidden()) return;
      for (const n of fresh) {
        alerts.show({ title: n.title, body: n.body || n.projectName, tag: n.id, onClick: () => this.open(n) });
      }
    },

    /**
     * Marks these read: at once on screen, then on the server.
     * @param {import("../domain/notification.js").Notification[]} targets
     */
    async markRead(targets) {
      const unread = targets.filter((n) => isUnread(n) && !pendingRead.has(n.id));
      if (unread.length === 0) return;
      const ids = unread.map((n) => n.id);
      const at = clock.now();
      for (const id of ids) pendingRead.add(id);
      this.pending = pendingRead.size;
      this.syncTitle();
      const read = unread.map((n) => markedRead(n, at));
      each((v) => v.apply(read));

      const request = { seenRead: new Set() };
      requests.add(request);
      try {
        const answer = await gateway.markRead({ ids });
        this.answered(answer, request);
      } catch (err) {
        // Nothing was marked: the rows go back as they were.
        each((v) => v.apply(unread.filter((n) => !request.seenRead.has(n.id))));
        if (codeOf(err) === Codes.NOTIFICATION_NOT_FOUND) each((v) => v.reload());
        throw err;
      } finally {
        requests.delete(request);
        for (const id of ids) pendingRead.delete(id);
        this.pending = pendingRead.size;
        this.syncTitle();
      }
    },

    /** Marks every unread one read, of one project or ("") of all. */
    async markAllRead(projectId = "") {
      const at = clock.now();
      pendingAll.set(projectId, (pendingAll.get(projectId) ?? 0) + 1);
      this.allPending = pendingAll.has("");
      this.syncTitle();
      each((v) => v.readAll(projectId, at));

      const request = { seenRead: new Set() };
      requests.add(request);
      try {
        const answer = await gateway.markRead(projectId ? { all: true, projectId } : { all: true });
        this.answered(answer, request);
      } catch (err) {
        each((v) => v.reload());
        throw err;
      } finally {
        requests.delete(request);
        const left = pendingAll.get(projectId) - 1;
        if (left > 0) pendingAll.set(projectId, left);
        else pendingAll.delete(projectId);
        this.allPending = pendingAll.has("");
        this.syncTitle();
      }
    },

    answered(answer, request) {
      for (const n of answer.notifications) seen.add(n.id);
      const echoed = answer.notifications.some((n) => request.seenRead.has(n.id));
      if (!echoed) this.serverCount = answer.unreadCount;
      if (answer.notifications.length) each((v) => v.apply(answer.notifications));
    },

    /**
     * Opens a notification at its link and marks it read. A click that opens
     * a new tab (a modifier, the middle button) keeps the page and only marks it.
     * @param {import("../domain/notification.js").Notification} n
     * @param {MouseEvent} [event]
     */
    async open(n, event) {
      if (event && (event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1)) {
        this.markRead([n]).catch(() => {});
        return;
      }
      event?.preventDefault();
      const read = this.markRead([n]).catch(() => {});
      await Promise.race([read, wait(OPEN_WAIT_MS)]);
      navigate(n.link);
    },

    /** The Desktop alerts switch: asks for permission only from the person's click. */
    async toggleAlerts() {
      if (this.alerts === "unsupported") return;
      if (this.alerts === "on") {
        this.alerts = "off";
        prefs.set(ALERTS_KEY, null);
        return;
      }
      const permission = alerts.permission() === "granted" ? "granted" : await alerts.request();
      if (permission === "granted") {
        this.alerts = "on";
        prefs.set(ALERTS_KEY, "on");
      } else {
        this.alerts = permission === "unsupported" ? "unsupported" : "denied";
      }
    },

    /** The unread count leads the document title: "(3) Notification · harness". */
    syncTitle() {
      if (!doc || !started) return;
      const n = this.unreadCount;
      doc.title = n ? `(${n}) ${baseTitle}` : baseTitle;
    },
  };
}
