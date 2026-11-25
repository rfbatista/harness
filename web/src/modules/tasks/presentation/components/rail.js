// The rail, live. The server renders the project's tasks in status groups,
// folded as the rail_collapsed cookie says; tasksRail renders the same groups
// from the shared store that tasksRailFeed (the project's feed, headless)
// keeps, so a task an agent creates or moves from its session regroups the
// rail as it happens, and a session started anywhere shows on its row as it
// starts. On top of the server's rail it adds the filter, Needs you, folding,
// the arrow keys, "/" and, below 48rem, the drawer.
//
//   <div class="rail" x-data="tasksRail" data-current-task="t1" …>
//     … <nav aria-label="Tasks"><div x-ignore data-ssr>…first paint…</div>
//     <template x-if="live"> … x-for="group in groups" … x-for="row in group.rows" …

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
/** Where a feature row's open state is kept in the browser's preferences. */
const featureKey = (id) => `rail-feature:${id}`;

const NAV_ITEMS = "h3 > button, a[data-task-id], .more > details > summary";

/** @typedef {import("./railFeed.js").RailStore} RailStore */

/** How many tasks need the person: a session waits, or a review does. */
export const needsYouCount = (store) => store.tasks.filter((t) => !t.archivedAt && needsYou(t, store.byTask[t.id])).length;

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
 *   store: RailStore,
 *   prefs: import("../../domain/ports.js").RailPrefs,
 *   browserPrefs?: import("../../../../shared/infrastructure/storage.js").Preferences,  where each feature row's open state is kept (ADR 0037)
 *   navigate: (url: string) => void,
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   isNarrow?: () => boolean,  the rail is a drawer (below 48rem)
 * }} deps
 */
export const rail = ({ store, prefs, browserPrefs = { get: () => null, set() {} }, navigate, timers = globalThis, isNarrow = () => globalThis.matchMedia?.("(width < 48rem)").matches ?? false }) => () => {
  /** The component's root: $el inside an event handler is the element that fired. */
  let root = null;
  let announcing = [];
  let announceTimer = null;
  let resultTimer = null;

  return {
    projectId: "",
    /** The open task's id, marked aria-current; "" when none. */
    currentTaskId: "",
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
    /** What the feed owner noted before a change, for after it. */
    pending: { waiting: new Set(), restoreFocus: () => {} },
    /** The feature rows the person opened or folded on this page; the rest read the browser's preference. */
    opened: {},

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
        open: (id) => this.isOpen(id),
      };
      return railGroups(store.tasks, { byTask: store.byTask, currentId: this.currentTaskId, collapsed: this.collapsed, query: this.query, needsYouOnly: this.needsYouOnly }).map((g) => toGroupView(g, ctx));
    },
    get total() {
      return store.tasks.filter((t) => !t.archivedAt).length;
    },
    get hasTasks() {
      return store.seeded && store.tasks.length > 0;
    },
    get isEmpty() {
      return store.seeded && store.tasks.length === 0;
    },
    /** Review requests waiting on the person across the project, for the Review requests badge. */
    get reviewsCount() {
      return store.tasks.reduce((n, t) => n + (t.archivedAt ? 0 : (t.pendingReviews ?? 0)), 0);
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
      this.currentTaskId = this.$el.dataset.currentTask ?? "";
      if (!store.seeded) return; // the server's rendering stays as it is
      this.projectId = store.projectId ?? "";
      this.collapsed = prefs.collapsed();
      this.live = true;
      // Alpine has rendered the live groups; drop the server-rendered copy and
      // bring the open task into view.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        reveal(this.$el.querySelector?.('.groups a[aria-current="page"]'));
      });
      // The feed owner (tasksRailFeed) tells the rail what each change was.
      store.observer = {
        before: () => {
          this.pending = { waiting: this.waitingIds(), restoreFocus: this.keepFocus() };
        },
        after: (change, previous) => {
          const { waiting, restoreFocus } = this.pending;
          if (change.kind === "task-upsert" && (!previous || previous.status !== change.task.status)) this.markFresh(change.task.id);
          this.noteWaiting(waiting);
          this.$nextTick(restoreFocus);
        },
      };
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

    /** A feature row is open: what the person chose on this page, else the browser's preference; folded by default. */
    isOpen(id) {
      return id in this.opened ? this.opened[id] : browserPrefs.get(featureKey(id)) === "open";
    },

    /** A feature row's twisty: shows or hides its children, and remembers it in this browser. */
    toggleFeature(event) {
      const id = event.currentTarget.dataset.featureId;
      const open = !this.isOpen(id);
      this.opened = { ...this.opened, [id]: open };
      browserPrefs.set(featureKey(id), open ? "open" : null);
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
      timers.clearTimeout(announceTimer);
      timers.clearTimeout(resultTimer);
      store.observer = undefined;
      store.drawerOpen = false;
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
