// The rail, live. The server renders the project's tasks in status groups,
// folded as the rail_collapsed cookie says; tasksRail follows the project's
// feed, keeps a shared store of the project's tasks and per-task activity, and
// renders the same groups from it, so a task an agent creates or moves from
// its session regroups the rail as it happens, and a session started anywhere
// shows on its row as it starts. The board (tasksBoard) draws its columns from
// the same store. On top of the server's rail it adds the filter, Needs you,
// folding, the arrow keys, "/" and, below 48rem, the drawer.
//
//   <div class="rail" x-data="tasksRail" data-seed="rail-seed" data-current-task="t1" data-reports-feed …>
//     … <nav aria-label="Tasks"><div x-ignore data-ssr>…first paint…</div>
//     <template x-if="live"> … x-for="group in groups" … x-for="row in group.rows" …
//
// data-reports-feed marks the rail as the page's feed (the project page):
// it then reports the connection to the stream bar as `feed-status`. The
// task page's own feed reports there instead.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { activityByTask, applyRailChange } from "../../domain/activity.js";
import { applyTaskChange, onBoard } from "../../domain/board.js";
import { needsYou, railGroups } from "../../domain/rail.js";
import { toGroupView } from "../railView.js";
import { FRESH_MS } from "./board.js";

/** How long a task that started waiting rings its dot. */
const SIGNAL_MS = 2_000;
/** Tasks that start waiting within this window are announced in one sentence. */
const ANNOUNCE_MS = 2_000;
/** The filter's result is read out once typing stops this long. */
const RESULT_MS = 500;

/** What the rail's arrow keys move between, in the Tasks nav. */
const NAV_ITEMS = "h3 > button, a[data-task-id], .more > details > summary";

/**
 * The store the rail writes and its rows, the Tasks button and the board read: Alpine.store("tasksRail").
 * @typedef {{
 *   byTask: Record<string, import("../../domain/activity.js").Activity>,
 *   tasks: import("../../domain/task.js").Task[],
 *   seeded: boolean,
 *   drawerOpen: boolean,  the phone drawer is open
 *   statusChanges?: Record<string, import("../../domain/board.js").StatusChange>,  the latest move per task seen over the feed
 *   archivedCount?: number,  the project's archived tasks, for "Archived (N)"
 * }} RailStore
 */

/** How many tasks need the person: a session waits, or a review does. */
export const needsYouCount = (store) => store.tasks.filter((t) => needsYou(t, store.byTask[t.id])).length;

/** Shown and focusable: not hidden, folded away or inside a closed <details>. */
const isShown = (el) => (el.checkVisibility ? el.checkVisibility() : el.getClientRects().length > 0);

/**
 * Scrolls the rail's list just enough to show a row. Not scrollIntoView: in
 * Chrome that also moves where Tab starts from, and the skip link must stay
 * the first Tab stop.
 */
function reveal(row) {
  const list = row?.closest?.(".groups");
  if (!list) return;
  const r = row.getBoundingClientRect();
  const box = list.getBoundingClientRect();
  if (r.bottom > box.bottom) list.scrollTop += r.bottom - box.bottom;
  else if (r.top < box.top) list.scrollTop -= box.top - r.top;
}

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").RailGateway,
 *   store: RailStore,
 *   prefs: import("../../domain/ports.js").RailPrefs,
 *   navigate: (url: string) => void,
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   isNarrow?: () => boolean,  the rail is a drawer (below 48rem)
 * }} deps
 */
export const rail = ({ gateway, store, prefs, navigate, timers = globalThis, isNarrow = () => globalThis.matchMedia?.("(width < 48rem)").matches ?? false }) => () => {
  let unfollow = () => {};
  let sessions = [];
  /** The component's root: $el inside an event handler is the element that fired. */
  let root = null;
  let announcing = [];
  let announceTimer = null;
  let resultTimer = null;
  /** The feed has been live once: changes before then (a merge state computed while the page loaded) were never delivered. */
  let wentLive = false;

  return {
    projectId: "",
    /** The open task's id, marked aria-current; "" when none. */
    currentTaskId: "",
    /** This rail is the page's feed (the project page): it reports the connection to the stream bar. */
    reportsFeed: false,
    /** The seed was read: the live copy renders and the server's goes. */
    live: false,
    query: "",
    /** Read out by the polite region under the filter. */
    filterResult: "",
    needsYouOnly: false,
    /** The folded groups' keys, as remembered in the cookie. */
    collapsed: [],
    /** A heading was clicked: from now on the chevron turns (not on the first paint). */
    turned: false,
    freshIds: [],
    signalIds: [],
    /** The task row with focus: it peeks out of a collapsed group. */
    focusedId: "",

    /** The rail's groups: every status in RAIL_ORDER, as the markup binds them. */
    get groups() {
      const ctx = {
        projectId: this.projectId,
        byTask: store.byTask,
        currentId: this.currentTaskId,
        fresh: new Set(this.freshIds),
        signal: new Set(this.signalIds),
        focusedId: this.focusedId,
        query: this.query,
      };
      return railGroups(store.tasks, { byTask: store.byTask, currentId: this.currentTaskId, collapsed: this.collapsed, query: this.query, needsYouOnly: this.needsYouOnly }).map((g) => toGroupView(g, ctx));
    },
    get total() {
      return store.tasks.length;
    },
    get hasTasks() {
      return store.seeded && store.tasks.length > 0;
    },
    get isEmpty() {
      return store.seeded && store.tasks.length === 0;
    },
    /** Review requests waiting on the person across the project, for the Review requests badge. */
    get reviewsCount() {
      return store.tasks.reduce((n, t) => n + (t.pendingReviews ?? 0), 0);
    },
    get hasReviews() {
      return this.reviewsCount > 0;
    },
    get needsYouCount() {
      return needsYouCount(store);
    },
    /** The toggle hides at 0, unless it is on: the view never switches mode by itself. */
    get showsNeedsYou() {
      return this.needsYouCount > 0 || this.needsYouOnly;
    },
    get matchedCount() {
      return this.groups.reduce((n, g) => n + g.rows.length, 0);
    },
    get hasNoMatch() {
      return this.query.trim() !== "" && this.matchedCount === 0;
    },
    get noMatchText() {
      return `No task matches “${this.query.trim()}”.`;
    },
    get nothingNeedsYou() {
      return this.needsYouOnly && this.query.trim() === "" && this.needsYouCount === 0;
    },
    get drawerOpen() {
      return store.drawerOpen;
    },

    init() {
      root = this.$el;
      this.reportsFeed = this.$el.dataset.reportsFeed !== undefined;
      this.currentTaskId = this.$el.dataset.currentTask ?? "";
      let seed;
      try {
        seed = gateway.decodeSeed(readSeed(this.$el));
      } catch {
        return; // the server's rendering stays as it is
      }
      this.projectId = seed.projectId;
      sessions = seed.sessions;
      store.tasks = onBoard(seed.tasks);
      store.archivedCount = seed.archivedCount;
      store.seeded = true;
      this.collapsed = prefs.collapsed();
      this.recount();
      this.live = true;
      // Alpine has rendered the live groups; drop the server-rendered copy and
      // bring the open task into view.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        reveal(this.$el.querySelector?.('.groups a[aria-current="page"]'));
      });
      unfollow = gateway.follow(
        this.projectId,
        (change) => this.apply(change),
        (status) => this.feedStatus(status),
      );
    },

    /**
     * The feed's state. On its first live, and on every resync, the lists are
     * read again: the seed was painted before the stream opened, and what the
     * server announced in between (a merge state it had just computed) never
     * reached this page.
     */
    feedStatus(status) {
      if (this.reportsFeed) this.$dispatch("feed-status", status);
      const first = status === FeedStatus.LIVE && !wentLive;
      if (status === FeedStatus.LIVE) wentLive = true;
      if (first || status === FeedStatus.RESYNCED) this.resync();
    },

    /** One feed change: a task's, or a session's. */
    apply(change) {
      if (change.kind === "task-status") {
        store.statusChanges = { ...store.statusChanges, [change.change.taskId]: change.change };
        return;
      }
      const waitingBefore = this.waitingIds();
      const restoreFocus = this.keepFocus();
      if (change.kind === "task-upsert" || change.kind === "task-deleted") {
        const id = change.kind === "task-deleted" ? change.id : change.task.id;
        const previous = store.tasks.find((t) => t.id === id) ?? null;
        store.tasks = applyTaskChange(store.tasks, change);
        this.countArchived(change, previous);
        if (change.kind === "task-upsert" && (!previous || previous.status !== change.task.status)) this.markFresh(id);
        this.$dispatch("task-changed", { change, previous });
      } else {
        sessions = applyRailChange(sessions, change);
        this.recount();
      }
      this.noteWaiting(waitingBefore);
      this.$nextTick(restoreFocus);
    },

    /**
     * Changes were missed while the stream was down: read everything again.
     * Sessions and tasks are refreshed independently, so one list failing
     * keeps the other current; whatever fails keeps what it shows until the
     * next resync. A resync is not news: no wash, no announcement.
     */
    async resync() {
      const [list, tasks] = await Promise.allSettled([gateway.listSessions(this.projectId), gateway.listTasks(this.projectId)]);
      if (list.status === "fulfilled") {
        sessions = list.value;
        this.recount();
      }
      if (tasks.status === "fulfilled") store.tasks = onBoard(tasks.value);
      this.recountArchived();
    },

    /**
     * Keeps "Archived (N)" in step: a task the store had that arrives
     * archived adds one. A done task the store did not have may be a restore
     * (or a new task); only the server knows, so the count is read again.
     */
    countArchived(change, previous) {
      if (change.kind !== "task-upsert") return;
      if (change.task.archivedAt && previous) store.archivedCount += 1;
      else if (!change.task.archivedAt && !previous && change.task.status === "done") this.recountArchived();
    },

    async recountArchived() {
      try {
        store.archivedCount = (await gateway.listArchived(this.projectId)).length;
      } catch {
        // keeps the count it shows until the next change or resync
      }
    },

    recount() {
      store.byTask = activityByTask(sessions);
    },

    waitingIds() {
      return new Set(store.tasks.filter((t) => needsYou(t, store.byTask[t.id])).map((t) => t.id));
    },

    /** Tasks that started waiting: their dot rings once, and the stream bar says so. */
    noteWaiting(before) {
      const started = store.tasks.filter((t) => needsYou(t, store.byTask[t.id]) && !before.has(t.id));
      for (const t of started) {
        this.signalIds = [...this.signalIds, t.id];
        timers.setTimeout(() => {
          this.signalIds = this.signalIds.filter((x) => x !== t.id);
        }, SIGNAL_MS);
        announcing.push(t.title);
      }
      if (announcing.length > 0 && announceTimer === null) {
        announceTimer = timers.setTimeout(() => this.announce(), ANNOUNCE_MS);
      }
    },

    announce() {
      const text = announcing.length === 1 ? `“${announcing[0]}” is waiting on you.` : `${announcing.length} tasks are waiting on you.`;
      announcing = [];
      announceTimer = null;
      this.$dispatch("rail-announce", text);
    },

    markFresh(id) {
      this.freshIds = [...this.freshIds.filter((x) => x !== id), id];
      timers.setTimeout(() => {
        this.freshIds = this.freshIds.filter((x) => x !== id);
      }, FRESH_MS);
    },

    // ── Folding, the filter, Needs you ─────────────────────────────────

    /** A heading's button: fold or unfold its group, and remember it. */
    toggleGroup(event) {
      const { key } = event.currentTarget.dataset;
      this.turned = true;
      const folding = !this.collapsed.includes(key);
      this.collapsed = folding ? [...this.collapsed, key] : this.collapsed.filter((k) => k !== key);
      prefs.setCollapsed(this.collapsed);
      // Folding the group a row has focus in (only possible with the mouse): focus its heading.
      const section = root?.querySelector?.(`.groups section[data-key="${key}"]`);
      if (folding && section?.querySelector("ul")?.contains(section.ownerDocument.activeElement)) {
        this.focusedId = "";
        section.querySelector("h3 > button")?.focus();
      }
    },

    toggleNeedsYou() {
      this.needsYouOnly = !this.needsYouOnly;
    },

    /** The filter's text changed: say how many match once typing stops. */
    filtered() {
      timers.clearTimeout(resultTimer);
      resultTimer = timers.setTimeout(() => {
        const n = this.matchedCount;
        if (this.query.trim() === "") this.filterResult = "";
        else if (n === 0) this.filterResult = "No task matches";
        else this.filterResult = n === 1 ? "1 task matches" : `${n} tasks match`;
      }, RESULT_MS);
    },

    filterKey(event) {
      if (event.key === "Escape" && this.query !== "") {
        event.preventDefault();
        this.clearQuery();
      } else if (event.key === "ArrowDown") {
        const first = this.navItems().find((el) => el.matches("a[data-task-id]"));
        if (first) {
          event.preventDefault();
          first.focus();
        }
      } else if (event.key === "Enter" && this.query.trim() !== "") {
        const first = this.groups.flatMap((g) => g.rows)[0];
        if (first) {
          event.preventDefault();
          navigate(first.href);
        }
      }
    },

    clearQuery() {
      timers.clearTimeout(resultTimer);
      this.query = "";
      this.filterResult = "";
    },

    /** The no-match notice's Clear: empty the field and go back to it. */
    clearFilter() {
      this.clearQuery();
      root?.querySelector?.(".filter input")?.focus();
    },

    // ── Keys ───────────────────────────────────────────────────────────

    /** The visible headings, rows and Show more of the Tasks nav, in order. */
    navItems() {
      const groups = root?.querySelector?.(".groups:not([data-ssr] *)");
      return groups ? [...groups.querySelectorAll(NAV_ITEMS)].filter(isShown) : [];
    },

    /** Arrow keys, Home and End in the Tasks nav (an enhancement: Tab still works). */
    navKey(event) {
      if (event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const items = this.navItems();
      const at = items.indexOf(event.target);
      if (at < 0) return;
      const heading = event.target.matches("h3 > button") ? event.target : null;
      const go = (el) => {
        if (!el) return;
        event.preventDefault();
        el.focus();
      };
      switch (event.key) {
        case "ArrowDown":
          return go(items[at + 1]);
        case "ArrowUp":
          return go(items[at - 1]);
        case "Home":
          return go(items[0]);
        case "End":
          return go(items.at(-1));
        case "ArrowLeft":
          if (!heading) return go(event.target.closest("section")?.querySelector("h3 > button"));
          if (heading.getAttribute("aria-expanded") === "true") {
            event.preventDefault();
            heading.click();
          }
          return;
        case "ArrowRight":
          if (!heading) return;
          if (heading.getAttribute("aria-expanded") === "false") {
            event.preventDefault();
            heading.click();
          } else {
            go(items[at + 1]?.closest("section") === heading.closest("section") ? items[at + 1] : null);
          }
          return;
      }
    },

    /** "/" anywhere but a text field, the terminal or an open dialog or popup: the filter. */
    globalKey(event) {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const target = event.target;
      if (target?.closest?.("input, textarea, select, [contenteditable=''], [contenteditable='true'], .xterm, dialog[open], [role=listbox], [role=menu]")) return;
      if (target?.ownerDocument?.querySelector?.("dialog[open]")) return;
      if (!this.hasTasks) return;
      event.preventDefault();
      const focusFilter = () => root?.querySelector?.(".filter input")?.focus();
      if (isNarrow() && !store.drawerOpen) {
        store.drawerOpen = true; // the drawer shows on the next render
        this.$nextTick(focusFilter);
      } else {
        focusFilter();
      }
    },

    // ── Focus during live changes ──────────────────────────────────────

    noteFocus(event) {
      this.focusedId = event.target.closest?.("a[data-task-id]")?.dataset.taskId ?? "";
    },

    forgetFocus(event) {
      const next = event.relatedTarget?.closest?.("a[data-task-id]");
      if (next?.dataset.taskId !== this.focusedId) this.focusedId = "";
    },

    /**
     * Before a live change, where focus should go after it: the same task
     * (in its new group, peeking if that is folded), else the next visible
     * item, else the previous, else the task's heading.
     */
    keepFocus() {
      const doc = root?.ownerDocument;
      const row = doc?.activeElement?.closest?.("a[data-task-id]");
      if (!row || !root.contains(row)) return () => {};
      const items = this.navItems();
      const at = items.indexOf(row);
      const where = (el) => {
        if (!el) return "";
        if (el.dataset.taskId) return `.groups a[data-task-id="${el.dataset.taskId}"]`;
        const key = el.closest("section")?.dataset.key;
        return el.matches("summary") ? `.groups section[data-key="${key}"] .more summary` : `.groups section[data-key="${key}"] h3 > button`;
      };
      const candidates = [where(row), where(items[at + 1]), where(items[at - 1]), where(row.closest("section")?.querySelector("h3 > button"))].filter(Boolean);
      return () => {
        if (root.contains(doc.activeElement) && doc.activeElement !== doc.body) return;
        for (const selector of candidates) {
          const el = [...root.querySelectorAll(selector)].find(isShown);
          if (el) return el.focus();
        }
      };
    },

    /** A truncated title shows whole as a native tooltip; the open task wraps instead. */
    tip(event) {
      const a = event.currentTarget;
      const label = a.querySelector(".label");
      if (label && label.scrollWidth > label.clientWidth && a.getAttribute("aria-current") !== "page") a.title = label.textContent;
      else a.removeAttribute("title");
    },

    // ── The phone drawer ───────────────────────────────────────────────

    openDrawer() {
      store.drawerOpen = true;
      this.$nextTick(() => {
        const target = root?.querySelector?.('.groups a[aria-current="page"]') ?? root?.querySelector?.(".filter input");
        reveal(target);
        target?.focus?.();
      });
    },

    closeDrawer() {
      if (!store.drawerOpen) return;
      store.drawerOpen = false;
      root?.ownerDocument?.querySelector(".tasks-toggle")?.focus();
    },

    /** While the drawer is open: Esc closes it, and Tab stays inside. */
    drawerKey(event) {
      if (!store.drawerOpen || event.defaultPrevented) return;
      if (event.key === "Escape") {
        event.preventDefault();
        this.closeDrawer();
        return;
      }
      if (event.key !== "Tab" || !root) return;
      const focusable = [...root.querySelectorAll("a[href], button:not([disabled]), input, summary")].filter((el) => isShown(el) && !el.closest("[data-ssr]"));
      const doc = root.ownerDocument;
      const first = focusable[0];
      const last = focusable.at(-1);
      if (event.shiftKey && doc.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && doc.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    },

    destroy() {
      unfollow();
      timers.clearTimeout(announceTimer);
      timers.clearTimeout(resultTimer);
    },
  };
};

/**
 * The phone's Tasks button and the drawer's scrim: they open and close the
 * rail's drawer, and the button counts what needs the person.
 * @param {{ store: RailStore }} deps
 */
export const railToggle = ({ store }) => () => ({
  get expanded() {
    return store.drawerOpen ? "true" : "false";
  },
  get openAttr() {
    return store.drawerOpen;
  },
  get needsYouCount() {
    return needsYouCount(store);
  },
  get hasNeedsYou() {
    return this.needsYouCount > 0;
  },
  /** Without JavaScript the button is a link to the board; with it, it opens the drawer. */
  open(event) {
    event?.preventDefault();
    this.$dispatch("rail-open");
  },
  close() {
    this.$dispatch("rail-close");
  },
});
