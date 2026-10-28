// The code views' state and actions, shared by the session's Code tab
// (codePanel) and the repository code page (codePage): which view is on
// screen (Changes, Files, Map), the source and whether it follows the
// worktree, the tree, the open file, the diff and the map. The rows of code
// are drawn by render/lines.js; everything around them is Alpine markup in
// internal/adapter/in/web/code.
//
// A view is a URL (UX §2.4): navigating pushes it, popstate restores it.
// The first paint is the server's (a seed in the wire shape); the component
// takes over without asking again for what was painted.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { baseName, changeMark } from "../../domain/code.js";
import { parseDiffHash, slugs, startsCollapsed } from "../../domain/diff.js";
import { fileRows, rowOfLineIn } from "../../domain/rows.js";
import { problemOf } from "../../domain/errors.js";
import { clampSplit, defaultSplit, parseSplit, splitBounds, stepSplit } from "../../domain/split.js";
import { clampRange, parseRange, parseRangeHash, rangeHash, scrollTopFor, SPLIT_UP_TO, splitLines, WRAP_UP_TO } from "../../domain/lines.js";
import { markParts, rank, shiftRanges } from "../../domain/fuzzy.js";
import { ANNOUNCE_CHANGED, FIXED, GONE, onChange, onEnded, onReload } from "../../domain/live.js";
import { PERMALINK_TOAST, hrefOf, parseHref, permalink, repositoryHref } from "../../domain/routes.js";
import { emptyTree, loadedOpenDirs, missingAncestors, openAction, revealPath, treeKey, typeAhead, visibleRows, withBusy, withExpanded, withLevel } from "../../domain/tree.js";
import { mapView, zoneIndex, zoneTags } from "../../domain/zones.js";
import { clearPending, drawLines, showPending } from "../render/lines.js";
import { intersectionProximity } from "../render/proximity.js";
import {
  crumbsView,
  diffFileView,
  diffSetup,
  fileMeta,
  placeholderOf,
  scopesView,
  skeletonRows,
  sourceBarView,
  summaryRowView,
  totalsView,
  treeRowView,
  truncatedNotice,
  viewsView,
} from "../view.js";

/** One frame spends at most this long drawing rows, a slice of this many at a time. */
export const DRAW_BUDGET_MS = 8;
export const SLICE_ROWS = 60;

/** Above these a file is shown plain (approved limit). */
export const HIGHLIGHT_BYTES = 256 * 1024;
export const HIGHLIGHT_LINES = 10_000;
const TYPEAHEAD_MS = 500;
const SEQUENCE_MS = 1_500;

/**
 * @typedef {object} BrowserDeps
 * @property {import("../../domain/ports.js").CodeGateway} gateway
 * @property {import("../../domain/ports.js").Highlighter} highlighter  the Files view's; a diff's is highlighted by the code worker
 * @property {import("../../domain/ports.js").DiffPreparer} preparer
 * @property {(root: Element|null, onChange: (changes: [Element, boolean][]) => void) => import("../render/proximity.js").Proximity} [proximity]  which file sections are near the viewport (an intersection observer by default)
 * @property {(cb: () => void) => void} [frame]  when the next slice of rows is drawn (requestAnimationFrame by default)
 * @property {number} [budgetMs]  how long one frame spends drawing rows
 * @property {import("../../../../shared/infrastructure/storage.js").Preferences} prefs
 * @property {{ pushState: Function, replaceState: Function }} [history]
 * @property {{ pathname: string, search: string, hash: string }} [location]
 * @property {(text: string) => Promise<void>} [copy]
 * @property {(cb: () => void) => void} [idle]
 * @property {(fn: Function, ms: number) => any} [setTimeout]
 */

/**
 * The part every code view composes. `this.kind` is "session" or
 * "repository"; the owner sets the identity fields before calling start().
 * @param {BrowserDeps} deps
 */
export function codeBrowser({ gateway, highlighter, preparer, proximity = intersectionProximity, frame = (cb) => requestAnimationFrame(cb), budgetMs = DRAW_BUDGET_MS, prefs, history = globalThis.history, location = globalThis.location, copy = defaultCopy, idle, setTimeout = globalThis.setTimeout.bind(globalThis) }) {
  // Not reactive: controllers of drawn lines, in-flight requests, timers.
  const drawn = new Map();
  // Lazy file sections: a section's lines are drawn when it comes near the viewport.
  /** @type {Map<string, { el: HTMLElement, ctl: ReturnType<typeof drawLines> }>} the blocks waiting for, or in the middle of, their rows */
  const blocks = new Map();
  /** The paths of the sections near the viewport, and the section element of each. */
  const near = new Set();
  const sectionPath = new Map();
  const queue = new Set();
  let nearby = null;
  let pumping = false;
  // The changed files' row views, kept while a file's data does not change: a selection redoes two.
  const rowViews = new Map();
  let heldSlugs = { readId: 0, slugs: {} };
  // The rows the code worker made for each diff read, by the read's id (Alpine
  // wraps the diff in a proxy, so the object itself is no key).
  /** @type {Map<number, { split: boolean, files: Map<string, import("../../domain/ports.js").PreparedFile> }>} */
  const rowsByRead = new Map();
  let reads = 0;
  const rowsOf = {
    get: (diff) => rowsByRead.get(diff?.readId),
    /** Keeps the rows of a diff, and drops those of reads nobody shows any more. */
    set(diff, sets, keep = []) {
      rowsByRead.set(diff.readId, sets);
      this.prune([diff, ...keep]);
    },
    /** Reads are numbered as they complete, and a read that completes later is shown later: only older ones than what is shown can go. */
    prune(shown) {
      const lowest = Math.min(...shown.map((d) => d?.readId).filter(Number.isInteger));
      for (const id of rowsByRead.keys()) if (id < lowest) rowsByRead.delete(id);
    },
  };
  let aborts = new Map();
  let zones = new Map();
  let unfollow = () => {};
  let pathsCache = null;
  let typed = "";
  let typedTimer = null;
  let sequence = "";
  let sequenceTimer = null;
  let scrollFrame = 0;
  let refreshing = null;
  // A file made current by ] or a click stays current through the scroll it causes,
  // even when the diff is too short to bring its head to the top.
  let pinnedUntil = 0;
  let refreshAgain = false;

  /** The queued block nearest the middle of the viewport; blocks that left the near zone are dropped. */
  function nearest(main) {
    const mid = main ? main.getBoundingClientRect().top + main.clientHeight / 2 : 0;
    let best = null;
    let bestD = Infinity;
    for (const path of queue) {
      const b = blocks.get(path);
      if (!b || !b.el.isConnected || b.ctl.complete) {
        queue.delete(path);
        continue;
      }
      if (!near.has(path)) {
        queue.delete(path);
        continue;
      }
      const d = Math.abs(b.el.getBoundingClientRect().top - mid);
      if (d < bestD) {
        best = path;
        bestD = d;
      }
    }
    return best;
  }

  function enqueue(path) {
    queue.add(path);
    if (!pumping) {
      pumping = true;
      frame(pump);
    }
  }

  /** One frame's drawing: slices of rows, nearest block first, until the frame's budget is spent. */
  function pump() {
    const main = self_.mainScroller?.() ?? null;
    const t0 = performance.now();
    do {
      const path = nearest(main);
      if (path === null) break;
      const b = blocks.get(path);
      const done = b.ctl.step(SLICE_ROWS);
      if (done) {
        clearPending(b.el);
        blocks.delete(path);
        queue.delete(path);
      }
    } while (queue.size && performance.now() - t0 < budgetMs);
    if (queue.size) frame(pump);
    else pumping = false;
  }

  const self_ = {
    kind: "repository",
    projectId: "",
    /** @type {import("../../domain/routes.js").CodeRoute} */
    route: null,
    /** @type {import("../../domain/ports.js").SourceQuery} */
    source: {},
    repositoryName: "",
    branch: "",
    baseRef: "",
    refs: [],
    historyHref: "",
    /** @type {import("../../domain/live.js").LiveState} */
    liveState: FIXED,
    commit: "",
    dirty: 0,
    /** The worktree is gone: Changes shows the branch, Files and Map an empty state. */
    gone: false,
    baseGone: false,

    tree: emptyTree(),
    focusedPath: "",
    freshPaths: [],
    /** @type {import("../../domain/code.js").CodeDiff|null} */
    diff: null,
    /** A re-read diff held while the reader is in the middle of the old one. */
    heldDiff: null,
    diffRev: 0,
    /** A file's section is drawn again when its counter moves: its content changed, or its withheld diff was loaded. */
    fileRevs: {},
    /** The paths that changed in reads the reader has not been shown yet (a held diff). */
    heldDirty: new Set(),
    /** The layout the open diff's rows were made for (the worker makes them; a switch waits for it). */
    rowsSplit: false,
    /** The first paint's diff page is being prepared. */
    seedPending: false,
    diffSlugs: {},
    collapsed: {},
    busyFiles: {},
    askedFiles: {},
    currentFile: "",
    /** @type {import("../../domain/code.js").CodeFile|null} */
    file: null,
    fileRev: 0,
    /** The entry opened that shows a placeholder (a symlink or a submodule). */
    openEntry: null,
    /** @type {import("../../domain/code.js").CodeMap|null} */
    map: null,
    target: null,
    targetSide: "R",
    /** @type {import("../../domain/errors.js").CodeProblem|null} */
    problem: null,
    loadingDiff: false,
    /** The whole diff's file count as the last totals read gave it, while no page is loaded (a hidden panel). */
    totalCount: null,
    /** The open diff (or none) is older than the worktree: a code_changed came while nobody looked at it. */
    diffStale: false,
    /** A later page of the diff is being read. */
    loadingPage: false,
    loadingFile: false,
    loadingMap: false,
    layout: "split",
    wrap: false,
    narrow: false,
    /** The layout's width in px and the root font size, as last measured. */
    layoutWidth: 0,
    layoutRem: 16,
    /** The chosen width of the file list, in percent of the layout; null: the default. */
    splitPercent: null,
    splitDragging: false,
    /** Full screen (⇧F): the page keeps only a thin bar over the diff. Never remembered. */
    fullscreen: false,
    /** In full screen, ⇧E hides the file list. */
    listHidden: false,
    /** Below 44rem: the main side is shown instead of the side list. */
    showMain: false,
    /** The reader scrolled the main side since it loaded. */
    touched: false,
    paletteMode: "",
    paletteQuery: "",
    paletteNote: "",
    paletteActive: 0,
    paths: null,
    pathsTruncated: false,
    announcement: "",
    toast: "",
    /** A session's Code tab takes the whole width (⇧E); the sessions page hides its list. */
    expanded: false,

    // ── the source bar ───────────────────────────────────────────────────
    get view() {
      const v = this.route?.view;
      if (v) return v;
      return this.kind === "session" ? (this.diff && this.diff.fileCount === 0 ? "files" : "changes") : "files";
    },
    get showingChanges() {
      return this.view === "changes";
    },
    get showingFiles() {
      return this.view === "files";
    },
    get showingMap() {
      return this.view === "map";
    },
    get bar() {
      return sourceBarView({ repositoryName: this.repositoryName, ref: this.kind === "session" ? this.branch : this.route?.ref || this.branch, commit: this.commit, dirty: this.dirty, state: this.liveState, kind: this.kind });
    },
    get hasState() {
      return this.liveState !== FIXED;
    },
    /** A diff or a page of it is being read: the bar's status spins and the changed files show skeleton rows. */
    get busy() {
      return this.loadingDiff || this.loadingPage;
    },
    /** The changed files list ends with Show more while pages remain. */
    get moreShown() {
      return !!this.diff?.next && !this.heldDiff;
    },
    /** The files left: the whole diff's count minus the files loaded. */
    get moreCount() {
      return this.diff ? String(Math.max(0, this.diff.fileCount - this.diff.files.length)) : "";
    },
    get busyAttr() {
      return this.busy ? "" : null;
    },
    get stateText() {
      return this.busy ? "Loading changes" : this.bar.word;
    },
    get listBusy() {
      return this.busy ? "true" : null;
    },
    get listSkeleton() {
      return this.busy ? skeletonRows("changed") : [];
    },
    get isStale() {
      return this.liveState === "stale";
    },
    get isGone() {
      return this.liveState === GONE;
    },
    get views() {
      return viewsView(this.route, { count: this.listDiff ? this.listDiff.fileCount : this.totalCount, kind: this.kind, current: this.view });
    },
    get scopes() {
      return scopesView(this.route, { scope: this.scope, gone: this.gone, baseGone: this.baseGone, baseRef: this.baseRef });
    },
    get scope() {
      if (this.gone) return "branch";
      if (this.baseGone) return "working";
      return this.route?.scope || "all";
    },
    get browseBranchHref() {
      return repositoryHref(this.projectId, this.source.repositoryId || this.diff?.source.repositoryId || "", this.branch);
    },
    get baseGoneLine() {
      return this.baseRef ? `The base branch ${this.baseRef} no longer resolves` : "The base branch no longer resolves";
    },

    // ── the side ─────────────────────────────────────────────────────────
    /**
     * The tree's rows, flat, in order: each with aria-level, -setsize and
     * -posinset, and a loading directory's skeleton rows right after it.
     */
    get treeRows() {
      const fresh = new Set(this.freshPaths);
      const focused = this.focusedPath || visibleRows(this.tree)[0]?.entry.path || "";
      const ctx = { state: this.tree, selected: this.file?.path ?? this.openEntry?.path ?? "", focused, zones, href: (p) => this.filesHref(p), fresh };
      const out = [];
      for (const row of visibleRows(this.tree)) {
        out.push(treeRowView(row, ctx));
        if (this.tree.busy[row.entry.path] && !this.tree.levels[row.entry.path]) {
          for (const s of skeletonRows(row.entry.path)) out.push({ ...s, skeleton: true, depthStyle: `--depth:${row.depth + 1}` });
        }
      }
      return out;
    },
    get treeBusy() {
      return this.tree.busy[""] ? "true" : null;
    },
    /** The changed files the side lists: the re-read diff while the open one is held (UX §5). */
    get listDiff() {
      return this.heldDiff ?? this.diff;
    },
    get summaryRows() {
      const list = this.listDiff;
      if (!list) return [];
      if (list !== this.diff && heldSlugs.readId !== list.readId) heldSlugs = { readId: list.readId, slugs: Object.fromEntries(slugs(list.files.map((f) => f.path))) };
      const bySlug = list === this.diff ? this.diffSlugs : heldSlugs.slugs;
      const fresh = new Set(this.freshPaths);
      const current = this.currentFile;
      return list.files.map((f) => {
        const slug = bySlug[f.path];
        const isFresh = fresh.has(f.path);
        const isCurrent = f.path === current;
        const hit = rowViews.get(f.path);
        if (hit && hit.f === f && hit.slug === slug && hit.fresh === isFresh && hit.current === isCurrent) return hit.view;
        const view = summaryRowView(f, { slug, current: isCurrent, fresh: isFresh });
        rowViews.set(f.path, { f, slug, fresh: isFresh, current: isCurrent, view });
        return view;
      });
    },
    get totals() {
      return this.listDiff ? totalsView(this.listDiff) : null;
    },
    get noChanges() {
      return !!this.diff && this.diff.fileCount === 0;
    },
    /** The empty diff's heading: what it was compared with. */
    get noChangesTitle() {
      if (this.scope === "working") return "No uncommitted changes";
      return this.baseRef ? `No changes against ${this.baseRef} yet` : "No changes yet";
    },
    get noChangesText() {
      return this.scope === "working" ? "No uncommitted changes." : "No changes on this branch yet. The agent's edits appear here as it makes them.";
    },

    // ── the main side ────────────────────────────────────────────────────
    get diffFiles() {
      if (!this.diff) return [];
      const sets = rowsOf.get(this.diff);
      return this.diff.files.map((f) => {
        const split = !!sets?.files.get(f.path)?.split;
        return diffFileView(f, {
          slug: this.diffSlugs[f.path],
          // The layout changes the rows: a new key draws them again.
          rev: `${this.fileRevs[f.path] ?? 0}:${this.rowsSplit ? "split" : "unified"}`,
          collapsed: !!this.collapsed[f.path],
          busy: !!this.busyFiles[f.path],
          asked: !!this.askedFiles[f.path],
          zones,
          diff: this.diff,
          viewHref: this.viewFileHref(f.path),
          size: undefined,
          splitAllowed: split,
        });
      });
    },
    /** Whether the layout switch and the window ask for side by side rows. */
    get wantSplit() {
      return this.layout === "split" && !this.narrow;
    },
    get truncatedDiff() {
      return !!this.diff?.truncated;
    },
    get canSplit() {
      return !this.narrow;
    },
    get sideBySideDisabled() {
      return this.canSplit ? null : "true";
    },
    /** Side by side's title while the window is too narrow for it. */
    get sideBySideTitle() {
      return this.canSplit ? null : "Side by side needs a wider window";
    },
    get unifiedPressed() {
      return String(this.layout !== "split");
    },
    get splitPressed() {
      return String(this.layout === "split");
    },
    get allCollapsed() {
      return !!this.diff && this.diff.files.length > 0 && this.diff.files.every((f) => this.collapsed[f.path]);
    },
    get collapseAllLabel() {
      return this.allCollapsed ? "Expand all" : "Collapse all";
    },
    get fileKeys() {
      // Wrapping changes how rows are drawn (not windowed): a new key draws them again.
      return this.file && this.file.content ? [`${this.file.path}@${this.fileRev}:${this.wrap && this.canWrap ? "wrap" : "nowrap"}`] : [];
    },
    get fileCrumbs() {
      const path = this.file?.path ?? this.openEntry?.path ?? "";
      return crumbsView(path, { rootName: this.repositoryName || "root", dirHref: (d) => this.filesHref(d) });
    },
    get fileZoneTags() {
      return this.file ? zoneTags(this.file.zones, zones) : { tags: [], more: null };
    },
    get fileMeta() {
      return this.file ? fileMeta(this.file, { highlighted: this.highlightable }) : "";
    },
    get highlightable() {
      return !!this.file && this.file.size <= HIGHLIGHT_BYTES && this.file.lineCount <= HIGHLIGHT_LINES;
    },
    get fileNotice() {
      return this.file ? truncatedNotice(this.file) : null;
    },
    get placeholder() {
      return placeholderOf({ file: this.file, entry: this.openEntry, href: (p) => this.filesHref(p) });
    },
    get hasFile() {
      return !!this.file || !!this.openEntry;
    },
    get filesEmpty() {
      return this.showingFiles && !this.gone && !this.hasFile && !this.loadingFile && !this.problem;
    },
    get filesLoading() {
      return this.showingFiles && !this.gone && this.loadingFile && !this.hasFile;
    },
    get filesShown() {
      return this.showingFiles && !this.gone && this.hasFile;
    },
    get mapLoading() {
      return this.showingMap && !this.gone && this.loadingMap && !this.map;
    },
    get mapNone() {
      return this.showingMap && !this.gone && this.mapEmpty;
    },
    get mapShown() {
      return this.showingMap && !this.gone && !!this.mapCards && !this.mapEmpty;
    },
    get mainLabel() {
      if (this.showingMap) return "Map";
      if (this.showingChanges) return "Diff";
      return this.file ? baseName(this.file.path) : "File";
    },
    get canWrap() {
      return !!this.file && this.file.lineCount <= WRAP_UP_TO;
    },
    get wrapTitle() {
      return this.canWrap ? "Wrap long lines (W)" : "Files over 5,000 lines do not wrap";
    },
    get wrapPressed() {
      return String(this.wrap && this.canWrap);
    },
    get mapCards() {
      return this.map ? mapView(this.map) : null;
    },
    get mapEmpty() {
      return !!this.mapCards && this.mapCards.empty;
    },
    zoneHref(dir) {
      return this.filesHref(dir);
    },
    get problemLine() {
      return this.problem && this.problem.as === "banner" ? `${this.problem.code} · ${this.problem.text}` : "";
    },
    get emptyProblem() {
      return this.problem && this.problem.as === "empty" ? this.problem : null;
    },
    get goneEmpty() {
      return this.gone && !this.showingChanges;
    },
    get paletteOpen() {
      return this.paletteMode !== "";
    },
    get finderRows() {
      if (this.paletteMode !== "file" || !this.paths) return [];
      return finderRows(this.paths, this.paletteQuery, this.paletteActive, this.diff);
    },
    get finderTruncated() {
      return this.paletteMode === "file" && this.pathsTruncated;
    },
    get paletteLabel() {
      return this.paletteMode === "line" ? "Go to line" : "Find file";
    },
    get palettePlaceholder() {
      return this.paletteMode === "line" ? "Line or range, like 120 or 120-140" : "Find a file by name or path";
    },

    // ── layout ───────────────────────────────────────────────────────────
    get mainAttr() {
      return this.showMain ? "" : null;
    },
    get sideAttr() {
      return this.showingMap || this.kind === "history" ? "none" : null;
    },
    get sideLabel() {
      return this.showingChanges ? "Changed files" : "Files";
    },
    get backLabel() {
      return this.showingChanges ? "‹ Changed files" : "‹ Files";
    },
    get hasRefs() {
      return this.kind === "repository" && this.refs.length > 0;
    },
    /** The ref select's options: the current ref always among them. */
    get refOptions() {
      const current = this.route?.ref || this.branch;
      const refs = current && !this.refs.includes(current) ? [current, ...this.refs] : this.refs;
      return refs.map((r) => ({ key: r, value: r, selected: r === current }));
    },
    /** Only a session's tab expands (codePanel narrows it to wide windows). */
    get canExpand() {
      return this.kind === "session";
    },
    get expandPressed() {
      return String(this.expanded);
    },
    get fullscreenPressed() {
      return String(this.fullscreen);
    },
    /** data-fullscreen on the view: present in full screen. */
    get fullscreenAttr() {
      return this.fullscreen ? "" : null;
    },
    /** data-list on the layout: "hidden" while full screen has put the file list away. */
    get listAttr() {
      return this.fullscreen && this.listHidden ? "hidden" : null;
    },
    get listToggleLabel() {
      return this.listHidden ? "Show file list" : "Hide file list";
    },
    /**
     * ⇧F and the Full screen / Exit buttons: the page hides everything but a
     * thin bar (it listens to code-fullscreen); the diff takes the focus, and
     * the button gets it back on exit. Same file, same scroll: nothing is redrawn.
     */
    toggleFullscreen() {
      if (this.kind !== "session") return;
      this.setFullscreen(!this.fullscreen);
    },
    setFullscreen(on) {
      if (on === this.fullscreen) return;
      this.fullscreen = on;
      this.$dispatch?.("code-fullscreen", { on });
      // Read before the page redraws: the Exit button that called this goes away, and a
      // magic read from a removed element finds no root. Then focus once the bar's
      // buttons have come or gone with the attribute.
      const main = this.$refs?.main;
      const root = this.$root;
      this.$nextTick?.(() =>
        setTimeout(() => (on ? main : root?.querySelector?.("[data-code-fullscreen]"))?.focus?.(), 0),
      );
    },
    /** ⇧E and the Expand button: the session list hides or comes back; the URL stays. */
    toggleExpand() {
      if (this.kind !== "session") return;
      if (this.fullscreen) {
        this.listHidden = !this.listHidden;
        return;
      }
      this.expanded = !this.expanded;
      this.$dispatch?.("code-expand", { expanded: this.expanded });
    },
    get paletteActiveId() {
      return this.paletteMode === "file" && this.finderRows.length ? `finder-${this.paletteActive}` : null;
    },
    get fileDigitsStyle() {
      return `--digits:${String(Math.max(1, this.file?.lineCount ?? 1)).length}`;
    },
    get wrapAttr() {
      return this.wrap && this.canWrap ? "" : null;
    },

    /** The danger banner's next step: the tree root, or the ref picker. */
    problemAction() {
      if (this.problem?.next === "root") this.showRoot();
      else if (this.problem?.next === "ref") this.$root?.querySelector?.(".code-source-bar select")?.focus();
    },

    /** Whether the open file is in the diff, for the file head's Changes link. */
    diffHas(path) {
      return !!this.diff?.files.some((f) => f.path === path);
    },
    changesHref(path) {
      return hrefOf({ ...this.route, view: "changes", path: "" }, `#diff-${this.diffSlugs[path] ?? ""}`);
    },
    async openChanges(event, path) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      await this.loadThrough(() => this.pathReached(path));
      this.go({ ...this.route, view: "changes", path: "" }, { hash: `diff-${this.diffSlugs[path] ?? ""}` });
      this.enterView();
      this.$nextTick?.(() => this.scrollToFile(path));
    },
    /** A symlink's Open the target. */
    openTarget(event) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      const href = event.currentTarget?.getAttribute("href") ?? "";
      const r = parseHref(new URL(href, "http://x").pathname, "");
      if (r?.path) this.openPath(r.path);
    },
    /** A zone of the map opens Files at its pattern's directory. */
    openZone(event, dir) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      this.go({ ...this.route, view: "files", path: dir });
      this.enterView();
      if (dir) this.openPath(dir, { push: false });
    },

    // ── hrefs ────────────────────────────────────────────────────────────
    /** A diff file head's View file; the history pages open the repository page instead. */
    viewFileHref(path) {
      return this.filesHref(path);
    },
    filesHref(path) {
      return hrefOf({ ...this.route, view: "files", path, scope: "" });
    },
    viewHref(view) {
      return hrefOf({ ...this.route, view, path: view === "files" ? (this.file?.path ?? "") : "", scope: view === "changes" ? this.route.scope : "" });
    },

    // ── start and stop ───────────────────────────────────────────────────
    /**
     * Takes over from the server's first paint.
     * @param {import("../../domain/ports.js").CodeSeed} seed
     */
    startFrom(seed) {
      this.projectId = seed.projectId;
      this.route = seed.route;
      this.source = seed.source;
      this.repositoryName = seed.repositoryName;
      this.branch = seed.branch;
      this.baseRef = seed.baseRef;
      this.refs = seed.refs;
      this.historyHref = seed.historyHref;
      this.layout = prefs.get("code.layout") === "unified" ? "unified" : "split";
      this.wrap = prefs.get("code.wrap") === "1";
      this.splitPercent = parseSplit(prefs.get("code.split"));
      if (seed.gone) this.markGone();
      this.baseGone = seed.baseGone;
      if (seed.map) this.setMap(seed.map);
      let tree = emptyTree();
      for (const t of seed.trees) tree = withExpanded(withLevel(tree, t.path, t.entries), t.path, t.path !== "");
      this.tree = tree;
      if (seed.trees[0]) this.commit = seed.trees[0].source.commit;
      // A repository page's ref as the server resolved it (its checkout's branch by default).
      if (this.kind === "repository" && !this.branch) this.branch = (seed.trees[0] ?? seed.file ?? seed.map)?.source.ref ?? "";
      if (seed.diffPage) this.adoptSeed(seed.diffPage);
      if (seed.file) this.setFile(seed.file);
      if (seed.error) this.fail(seed.error, { path: seed.route.path, ref: seed.route.ref });
      if (seed.route.view === "files" && seed.route.path && !seed.file && !seed.error) this.openPath(seed.route.path, { push: false });
      this.readHash(location?.hash ?? "");
    },

    /** The server painted the first diff page: the worker prepares it, no first read. */
    adoptSeed(page) {
      this.seedPending = true;
      this.loadingDiff = true;
      this.prepareDiff(page)
        .then((diff) => {
          this.setDiff(diff);
          // A link to a file arrives with the page, before its files are known.
          this.readHash(location?.hash ?? "");
        })
        .catch((err) => this.fail(err, {}))
        .finally(() => {
          this.seedPending = false;
          this.loadingDiff = false;
        });
    },

    stop() {
      nearby?.disconnect();
      nearby = null;
      near.clear();
      queue.clear();
      blocks.clear();
      sectionPath.clear();
      unfollow();
      for (const c of drawn.values()) c.destroy();
      drawn.clear();
      for (const a of aborts.values()) a.abort();
      aborts = new Map();
    },

    /** Follows the project feed for this worktree's code_changed. */
    follow() {
      unfollow();
      if (!this.source.workspaceId) return;
      unfollow = gateway.follow(
        this.projectId,
        (change) => {
          if (change.workspaceId === this.source.workspaceId) this.codeChanged();
        },
        (status) => {
          if (status === FeedStatus.RESYNCED) this.codeChanged();
        },
      );
    },

    /** The session ended: the code is fixed. */
    ended() {
      unfollow();
      this.liveState = onEnded(this.liveState);
    },

    // ── reading ──────────────────────────────────────────────────────────
    request(name) {
      aborts.get(name)?.abort();
      const ctl = new AbortController();
      aborts.set(name, ctl);
      return ctl.signal;
    },

    setMap(map) {
      this.map = map;
      zones = zoneIndex(map);
    },

    setDiff(diff) {
      const { slugs: bySlug, collapsed } = diffSetup(diff);
      // The files whose content differs from what the reader has: only their sections are drawn again.
      const prior = this.diff;
      const dirty = new Set(this.heldDirty);
      for (const path of changedPaths(rowsOf.get(diff))) dirty.add(path);
      this.heldDirty = new Set();
      if (prior) {
        const revs = { ...this.fileRevs };
        for (const f of diff.files) if (dirty.has(f.path)) revs[f.path] = (revs[f.path] ?? 0) + 1;
        this.fileRevs = revs;
        this.askedFiles = Object.fromEntries(Object.entries(this.askedFiles).filter(([p]) => !dirty.has(p)));
      } else {
        this.fileRevs = {};
        this.askedFiles = {};
      }
      const keep = this.diff ? { ...this.collapsed } : {};
      this.diffSlugs = Object.fromEntries(bySlug);
      this.collapsed = this.diff ? Object.fromEntries(diff.files.filter((f) => keep[f.path] ?? collapsed[f.path]).map((f) => [f.path, true])) : collapsed;
      this.diff = diff;
      this.diffRev += 1;
      this.rowsSplit = rowsOf.get(diff)?.split ?? this.wantSplit;
      rowsOf.prune([diff, this.heldDiff]);
      if (diff.source.gone) this.markGone();
      if (!this.commit || diff.source.kind === "workspace") this.commit = diff.source.commit;
      if (!this.currentFile || !diff.files.some((f) => f.path === this.currentFile)) this.currentFile = diff.files[0]?.path ?? "";
      this.countFiles();
      this.syncRows();
    },

    /** Reads a diff as the server answers it and has the code worker prepare it; its rows are kept beside it. */
    async prepareDiff(raw) {
      const files = new Map();
      const split = this.wantSplit;
      const read = await preparer.prepare(raw, { split }, { onFile: (f) => files.set(f.path, f) });
      const diff = Object.freeze({ ...read, readId: ++reads });
      rowsByRead.set(diff.readId, { split, files });
      return diff;
    },

    /** What the diff is read by: the worktree's scope, or the history page's commit. */
    pageQuery() {
      return this.kind === "history" ? this.historyQuery() : { workspaceId: this.source.workspaceId, scope: this.scope };
    },

    /** Show more, and the scroll near the end: reads the next page and appends it. False when nothing was read. */
    async loadMore() {
      const open = this.diff;
      if (!open?.next || this.loadingPage || this.heldDiff) return false;
      this.loadingPage = true;
      try {
        const page = await this.prepareDiff(await gateway.diffPage({ ...this.pageQuery(), after: open.next }, this.request("page")));
        // A refresh or a scope change replaced the diff while the page was on its way.
        if (this.diff !== open) return false;
        this.appendPage(page);
        return true;
      } catch (err) {
        if (err?.name !== "AbortError") this.fail(err, {});
        return false;
      } finally {
        this.loadingPage = false;
      }
    },

    /** A diff with a page's files after its own; a file seen twice (the worktree moved between reads, P6) takes the newer. */
    mergeDiff(open, page) {
      const sets = rowsOf.get(open);
      const at = new Map(open.files.map((f, i) => [f.path, i]));
      const files = [...open.files];
      for (const f of page.files) {
        if (at.has(f.path)) files[at.get(f.path)] = f;
        else files.push(f);
      }
      const merged = Object.freeze({ ...open, files: Object.freeze(files), next: page.next, fileCount: page.fileCount, added: page.added, deleted: page.deleted, truncated: open.truncated || page.truncated, readId: ++reads });
      rowsByRead.set(merged.readId, { split: sets.split, files: new Map([...sets.files, ...rowsOf.get(page).files]) });
      return merged;
    },

    /** Adds a page's files after the loaded ones. */
    appendPage(page) {
      const open = this.diff;
      const at = new Set(open.files.map((f) => f.path));
      const next = this.mergeDiff(open, page);
      rowsOf.prune([next, this.heldDiff]);
      const { slugs: bySlug, collapsed } = diffSetup(next);
      this.diffSlugs = Object.fromEntries(bySlug);
      this.collapsed = { ...Object.fromEntries(page.files.filter((f) => collapsed[f.path] && !(f.path in this.collapsed) && !at.has(f.path)).map((f) => [f.path, true])), ...this.collapsed };
      // A file the worktree changed between two page reads is drawn again.
      const again = page.files.filter((f) => at.has(f.path) && rowsOf.get(page)?.files.get(f.path)?.changed);
      if (again.length) this.fileRevs = { ...this.fileRevs, ...Object.fromEntries(again.map((f) => [f.path, (this.fileRevs[f.path] ?? 0) + 1])) };
      this.diff = next;
      this.countFiles();
      this.syncRows();
    },

    /** Reads pages until `loaded()` holds or there are no more. */
    async loadThrough(loaded) {
      while (this.diff?.next && !loaded()) if (!(await this.loadMore())) break;
    },
    /** Whether the path is among the loaded files, or past where its place would be (it is not changed). */
    pathReached(path) {
      const files = this.diff?.files ?? [];
      return files.some((f) => f.path === path) || (files.length > 0 && files.at(-1).path > path);
    },
    pathOfSlug(slug) {
      return Object.keys(this.diffSlugs).find((p) => this.diffSlugs[p] === slug);
    },

    /** The open diff's rows follow the layout: asked of the worker again when they were made for the other one. */
    async syncRows() {
      const want = this.wantSplit;
      const open = this.diff;
      const sets = open && rowsOf.get(open);
      // A held diff is not the worker's last read; its rows stay as they are until it is applied.
      if (!sets || this.heldDiff || sets.split === want) return;
      const files = new Map(sets.files);
      try {
        await preparer.relayout({ split: want }, { onFile: (f) => open.files.some((x) => x.path === f.path) && files.set(f.path, f) });
      } catch {
        return;
      }
      if (this.diff !== open || this.wantSplit !== want) return;
      rowsOf.set(open, { split: want, files }, [this.heldDiff]);
      this.rowsSplit = want;
    },

    /** The session's tab badge follows the changed files. */
    countFiles() {
      const count = this.diff ? this.diff.fileCount : this.totalCount;
      if (this.kind === "session" && count !== null) this.$dispatch?.("code-files-counted", { count });
    },

    /** Nobody is looking at the diff (the owner overrides: a panel behind another tab). */
    get background() {
      return false;
    },

    /** The diff totals alone (limit 0): what a hidden panel reads for the tab's count. */
    async readTotals() {
      if (this.gone || !this.source.workspaceId) return;
      try {
        const t = await gateway.diff({ workspaceId: this.source.workspaceId, scope: this.scope, limit: 0 }, this.request("totals"));
        this.totalCount = t.fileCount;
        if (!this.diff) this.countFiles();
      } catch (err) {
        if (codeOf(err) === Codes.WORKSPACE_MISSING) this.markGone();
        // the count is a hint; the tab goes without it
      }
    },

    setFile(file) {
      this.file = file;
      this.openEntry = null;
      this.fileRev += 1;
      this.commit = file.source.commit || this.commit;
      if (this.file.binary || !this.file.content) this.target = null;
    },

    async loadDiff({ quiet = false } = {}) {
      if (!this.source.workspaceId && this.kind !== "history") return;
      const signal = this.request("diff");
      if (!quiet) this.loadingDiff = true;
      try {
        return await this.prepareDiff(await gateway.diffPage({ workspaceId: this.source.workspaceId, scope: this.scope }, signal));
      } catch (err) {
        if (err?.name === "AbortError") return null;
        if (codeOf(err) === Codes.WORKSPACE_MISSING) {
          this.markGone();
          return this.loadDiff({ quiet });
        }
        if (codeOf(err) === Codes.REF_NOT_FOUND && this.scope !== "working") {
          this.baseGone = true;
          return this.loadDiff({ quiet });
        }
        this.fail(err, {});
        return null;
      } finally {
        if (!quiet) this.loadingDiff = false;
      }
    },

    /** The loaded pages again, first page first: a refresh reads as far as the reader had read, and no further. */
    async loadLoaded({ quiet = false } = {}) {
      const want = this.diff?.files.length ?? 0;
      let acc = await this.loadDiff({ quiet });
      try {
        while (acc && acc.next && acc.files.length < want) {
          const page = await this.prepareDiff(await gateway.diffPage({ ...this.pageQuery(), after: acc.next }, this.request("diff")));
          acc = this.mergeDiff(acc, page);
        }
      } catch (err) {
        // The pages after the first are read again when reached.
        if (err?.name === "AbortError") return null;
      }
      return acc;
    },

    async showDiff() {
      if (this.seedPending) return;
      this.diffStale = false;
      const diff = await this.loadDiff();
      if (diff) {
        this.setDiff(diff);
        this.problem = null;
      }
    },

    async loadDirectory(dir) {
      if (this.gone) return;
      this.tree = withBusy(withExpanded(this.tree, dir, true), dir, true);
      try {
        const t = await gateway.tree(this.source, dir, this.request(`tree:${dir}`));
        this.tree = withLevel(this.tree, dir, t.entries);
        if (dir === "") this.commit = t.source.commit;
      } catch (err) {
        if (err?.name === "AbortError") return;
        this.tree = withBusy(withExpanded(this.tree, dir, false), dir, false);
        this.fail(err, { path: dir, ref: this.route.ref });
      }
    },

    async ensureMap() {
      if (this.map || this.loadingMap || this.gone) return;
      this.loadingMap = true;
      try {
        this.setMap(await gateway.map(this.source, this.request("map")));
      } catch (err) {
        if (err?.name !== "AbortError") this.fail(err, {});
      } finally {
        this.loadingMap = false;
      }
    },

    fail(err, about) {
      const code = typeof err?.code === "string" ? err.code : codeOf(err);
      if (code === Codes.WORKSPACE_MISSING) {
        this.markGone();
        return;
      }
      this.problem = problemOf(code, { ...about, repository: this.repositoryName, defaultRef: this.kind === "repository" && !this.route?.ref });
    },

    markGone() {
      if (this.gone) return;
      this.gone = true;
      this.liveState = GONE;
      unfollow();
    },

    // ── navigating ───────────────────────────────────────────────────────
    go(route, { push = true, hash = "" } = {}) {
      this.route = route;
      if (this.urlOwned()) history?.[push ? "pushState" : "replaceState"]?.(null, "", hrefOf(route, hash));
    },
    /** What scrolls the diff: the main side (the history pages scroll their own pane). */
    mainScroller() {
      return this.$refs?.main ?? null;
    },
    /** Whether this view owns the address bar (the session's tab only while in front). */
    urlOwned() {
      return true;
    },

    showView(view) {
      if (view === this.view && this.route.view === view) return;
      this.go({ ...this.route, view, path: view === "files" ? (this.file?.path ?? this.route.path) : "", scope: view === "changes" ? this.route.scope : "" });
      this.problem = null;
      this.enterView();
    },
    /** Loads what the view on screen needs. */
    enterView() {
      if (this.showingChanges && (!this.diff || this.diffStale) && !this.seedPending) this.showDiff();
      if (this.showingFiles && !this.tree.levels[""] && !this.gone) this.loadDirectory("");
      if (this.showingMap || this.showingFiles || this.showingChanges) this.ensureMap();
    },
    clickView(event, view) {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1) return;
      event.preventDefault();
      this.showView(view);
    },
    clickScope(event, scope) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      if (event.currentTarget?.getAttribute("aria-disabled") === "true") return;
      if (scope === this.scope) return;
      this.go({ ...this.route, view: "changes", scope });
      this.diff = null;
      this.showDiff();
    },

    /** Opens a path of the tree, a file from Find file, or a path from the URL. */
    async openPath(path, { push = true, hash = "" } = {}) {
      this.showMain = true;
      this.problem = null;
      if (!this.tree.levels[""]) await this.loadDirectory("");
      for (const dir of missingAncestors(this.tree, path)) await this.loadDirectory(dir);
      this.tree = revealPath(this.tree, path);
      const entry = this.entryOf(path);
      const action = entry ? openAction(entry) : "file";
      if (action === "toggle") {
        this.go({ ...this.route, view: "files", path }, { push });
        if (!this.tree.levels[path]) await this.loadDirectory(path);
        this.tree = withExpanded(this.tree, path, true);
        this.focusedPath = path;
        return;
      }
      if (action === "diff") {
        // A deleted file is not on disk (C8): its diff, not /file.
        if (!this.diff) await this.showDiff();
        await this.loadThrough(() => this.pathReached(path));
        this.go({ ...this.route, view: "changes", path: "", scope: this.route.scope }, { push, hash: `diff-${this.diffSlugs[path] ?? ""}` });
        this.$nextTick?.(() => this.scrollToFile(path));
        return;
      }
      this.go({ ...this.route, view: "files", path }, { push, hash });
      this.focusedPath = path;
      if (action === "symlink" || action === "submodule") {
        this.file = null;
        this.openEntry = entry;
        return;
      }
      await this.loadFile(path);
    },

    entryOf(path) {
      for (const entries of Object.values(this.tree.levels)) {
        const e = entries.find((x) => x.path === path);
        if (e) return e;
      }
      return null;
    },

    async loadFile(path) {
      this.loadingFile = true;
      try {
        const file = await gateway.file(this.source, path, this.request("file"));
        this.setFile(file);
        this.touched = false;
      } catch (err) {
        if (err?.name === "AbortError") return;
        this.file = null;
        this.fail(err, { path, ref: this.route.ref || this.branch });
      } finally {
        this.loadingFile = false;
      }
    },

    clickFile(event, path) {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1) return;
      event.preventDefault();
      this.openPath(path);
    },

    showRoot() {
      this.problem = null;
      this.file = null;
      this.openEntry = null;
      this.go({ ...this.route, view: "files", path: "" });
      if (!this.tree.levels[""]) this.loadDirectory("");
    },

    back() {
      this.showMain = false;
    },

    /** The browser went back or forward: show what the URL says. */
    popState() {
      const r = parseHref(location.pathname, location.search);
      if (!r || r.kind !== this.route.kind) return;
      const viewChanged = r.view !== this.route.view || r.scope !== this.route.scope;
      this.route = r;
      if (r.view === "files" && r.path && r.path !== this.file?.path) this.openPath(r.path, { push: false });
      else if (viewChanged) {
        if (r.view === "changes") this.showDiff();
        this.enterView();
      }
      this.readHash(location.hash);
    },

    async readHash(hash) {
      const diffHash = parseDiffHash(hash);
      if (diffHash) {
        // A file on a page not loaded yet: read pages up to it.
        await this.loadThrough(() => !!this.pathOfSlug(diffHash.slug));
        const path = this.pathOfSlug(diffHash.slug);
        if (path) {
          this.currentFile = path;
          if (diffHash.line) {
            this.target = { start: diffHash.line, end: diffHash.line };
            this.targetSide = diffHash.side || "R";
          }
          this.$nextTick?.(() => this.scrollToFile(path, diffHash.line, diffHash.side));
        }
        return;
      }
      const range = parseRangeHash(hash);
      if (range) {
        this.target = range;
        this.$nextTick?.(() => this.scrollToLine(range.start));
      }
    },

    // ── the tree ─────────────────────────────────────────────────────────
    async toggleDir(path) {
      if (this.tree.expanded[path]) {
        this.tree = withExpanded(this.tree, path, false);
        return;
      }
      if (!this.tree.levels[path]) await this.loadDirectory(path);
      else this.tree = withExpanded(this.tree, path, true);
    },

    clickRow(event, path) {
      const entry = this.entryOf(path);
      if (!entry) return;
      this.focusedPath = path;
      if (entry.kind === "dir") {
        event.preventDefault();
        this.toggleDir(path);
        return;
      }
      this.clickFile(event, path);
    },

    treeKeydown(event) {
      const key = event.key;
      if (key === "Enter" || key === " ") {
        const entry = this.entryOf(this.focusedPath);
        if (!entry) return;
        event.preventDefault();
        if (entry.kind === "dir") this.toggleDir(entry.path);
        else this.openPath(entry.path);
        return;
      }
      const move = treeKey(this.tree, this.focusedPath, key);
      if (Object.keys(move).length > 0 || ["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Home", "End", "*"].includes(key)) {
        event.preventDefault();
        event.stopPropagation();
      }
      if (move.focus) this.focusRow(move.focus);
      if (move.expand) this.toggleDir(move.expand);
      if (move.collapse) this.tree = withExpanded(this.tree, move.collapse, false);
      if (move.expandAll) for (const dir of move.expandAll) this.toggleDir(dir);
      if (Object.keys(move).length === 0 && key.length === 1 && /\S/.test(key) && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        event.stopPropagation();
        clearTimeout(typedTimer);
        typed += key;
        typedTimer = setTimeout(() => (typed = ""), TYPEAHEAD_MS);
        const found = typeAhead(this.tree, this.focusedPath, typed);
        if (found) this.focusRow(found);
      }
    },

    focusRow(path) {
      this.focusedPath = path;
      this.$nextTick?.(() => {
        const el = this.$root?.querySelector?.(`[role="treeitem"][title="${cssEscape(path)}"] > .item`);
        el?.focus();
      });
    },

    // ── the diff ─────────────────────────────────────────────────────────
    toggleFile(path) {
      this.collapsed = { ...this.collapsed, [path]: !this.collapsed[path] };
      if (!this.collapsed[path]) {
        const next = { ...this.collapsed };
        delete next[path];
        this.collapsed = next;
      }
    },
    toggleAll() {
      if (!this.diff) return;
      this.collapsed = this.allCollapsed ? {} : Object.fromEntries(this.diff.files.map((f) => [f.path, true]));
    },
    setLayout(layout) {
      if (this.narrow && layout === "split") return;
      this.layout = layout;
      prefs.set("code.layout", layout === "split" ? "split" : "unified");
      this.syncRows();
    },
    toggleWrap() {
      if (!this.canWrap) return;
      this.wrap = !this.wrap;
      prefs.set("code.wrap", this.wrap ? "1" : null);
    },

    /** Load diff: a withheld file, asked for alone (path=). */
    async loadWithheld(path) {
      if (!this.diff) return;
      this.busyFiles = { ...this.busyFiles, [path]: true };
      try {
        const q = this.kind === "history" ? this.historyQuery() : { workspaceId: this.source.workspaceId, scope: this.scope };
        const one = await this.prepareDiff(await gateway.diffPage({ ...q, path }, this.request(`diff:${path}`)));
        const f = one.files[0];
        if (f) {
          const sets = rowsOf.get(this.diff);
          const next = Object.freeze({ ...this.diff, readId: ++reads, files: Object.freeze(this.diff.files.map((x) => (x.path === path ? f : x))) });
          rowsOf.set(next, { split: sets.split, files: new Map([...sets.files, ...rowsOf.get(one).files]) }, [this.heldDiff]);
          this.diff = next;
          // Only this file's section is drawn again.
          this.fileRevs = { ...this.fileRevs, [path]: (this.fileRevs[path] ?? 0) + 1 };
          if (f.withheld) this.askedFiles = { ...this.askedFiles, [path]: true };
        }
      } catch (err) {
        if (err?.name !== "AbortError") this.fail(err, { path });
      } finally {
        const busy = { ...this.busyFiles };
        delete busy[path];
        this.busyFiles = busy;
      }
    },
    historyQuery() {
      return {};
    },

    /**
     * A file's section is in the page (x-init on the section): watches whether it is
     * near the viewport, and draws its lines then.
     */
    watchFile(el, path) {
      nearby ??= proximity(this.mainScroller() ?? null, (changes) => {
        for (const [target, isNear] of changes) {
          const p = sectionPath.get(target);
          if (p === undefined) continue;
          if (!target.isConnected) {
            nearby.unwatch(target);
            sectionPath.delete(target);
            continue;
          }
          if (isNear) {
            near.add(p);
            if (blocks.has(p)) enqueue(p);
          } else near.delete(p);
        }
      });
      sectionPath.set(el, path);
      nearby.watch(el);
    },

    /**
     * A file's .code-lines is in the page (x-init on it): it holds skeleton rows of
     * the height its rows will have until its section is near, then the rows are
     * drawn into it in slices.
     */
    drawHunks(el, path) {
      const prepared = rowsOf.get(this.diff)?.files.get(path);
      drawn.get(`diff:${path}`)?.destroy();
      blocks.delete(path);
      if (!prepared) return;
      const ctl = drawLines(el, {
        rows: prepared.rows,
        scroller: this.mainScroller() ?? null,
        slug: this.diffSlugs[path],
        // The worker put the tokens on the rows.
        virtual: !prepared.split,
        target: this.currentFile === path ? this.target : null,
        targetSide: this.targetSide,
        idle,
        deferred: true,
      });
      drawn.set(`diff:${path}`, ctl);
      showPending(el, prepared.rows.length);
      blocks.set(path, { el, ctl });
      if (near.has(path)) enqueue(path);
    },

    /** Draws a file's rows now, whole (a link, a key or a line asked for them). */
    drawNow(path) {
      const b = blocks.get(path);
      if (!b || b.ctl.complete) return;
      b.ctl.finish();
      clearPending(b.el);
      blocks.delete(path);
      queue.delete(path);
    },

    /** Draws the open file's lines (x-init on its .code-lines). */
    drawFile(el) {
      drawn.get("file")?.destroy();
      if (!this.file?.content) return;
      const ctl = drawLines(el, {
        rows: fileRows(splitLines(this.file.content)),
        scroller: this.$refs?.scroll ?? null,
        highlighter: this.highlightable ? highlighter : null,
        language: this.file.language,
        virtual: !(this.wrap && this.canWrap),
        target: this.target,
        idle,
      });
      drawn.set("file", ctl);
      if (this.target) this.$nextTick?.(() => this.scrollToLine(this.target.start));
    },

    clickLine(event) {
      const num = event.target?.closest?.("a.num[data-line]");
      if (!num || event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      const n = Number(num.dataset.line);
      const href = num.getAttribute("href") ?? "";
      const diffHash = parseDiffHash(href);
      if (diffHash) {
        const path = Object.keys(this.diffSlugs).find((p) => this.diffSlugs[p] === diffHash.slug);
        this.currentFile = path ?? this.currentFile;
        this.target = event.shiftKey && this.target && this.targetSide === diffHash.side ? spanTo(this.target, n) : { start: n, end: n };
        this.targetSide = diffHash.side;
        if (this.urlOwned()) history?.replaceState?.(null, "", hrefOf(this.route, href));
        drawn.get(`diff:${path}`)?.setTarget(this.target, this.targetSide);
        return;
      }
      this.target = event.shiftKey && this.target ? spanTo(this.target, n) : { start: n, end: n };
      if (this.urlOwned()) history?.replaceState?.(null, "", hrefOf(this.route, rangeHash(this.target)));
      drawn.get("file")?.setTarget(this.target);
    },

    /** Scrolls the main side to a file of the diff, its head at the top, and makes it current. */
    scrollToFile(path, line = 0, side = "R") {
      const main = this.mainScroller();
      const slug = this.diffSlugs[path];
      if (!slug) return;
      this.currentFile = path;
      if (!main) return;
      pinnedUntil = Date.now() + 400;
      if (this.collapsed[path] && line) this.toggleFile(path);
      const section = main.querySelector?.(`#diff-${cssEscape(slug)}`);
      if (!section) return;
      let top = topIn(main, section);
      if (line) {
        this.drawNow(path);
        const ctl = drawn.get(`diff:${path}`);
        const row = rowOfLineIn(rowsOf.get(this.diff)?.files.get(path)?.rows ?? [], side, line);
        if (ctl && row >= 0) top = ctl.blockOffset() + ctl.offsetOf(row) - Math.floor(main.clientHeight / 3);
      }
      main.scrollTop = Math.max(0, top);
      drawn.get(`diff:${path}`)?.refresh();
    },

    scrollToLine(n) {
      const scroll = this.$refs?.scroll;
      const ctl = drawn.get("file");
      if (!scroll || !ctl) return;
      scroll.scrollTop = scrollTopFor({ index: n - 1, rowHeight: ctl.offsetOf(1) - ctl.offsetOf(0) || 20, viewport: scroll.clientHeight });
      ctl.refresh();
    },

    async clickSummary(event, path) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      this.showMain = true;
      // A file only the re-read diff has: show that diff first.
      if (this.heldDiff && !this.diff?.files.some((f) => f.path === path)) await this.reload();
      if (this.collapsed[path]) this.toggleFile(path);
      this.$nextTick?.(() => this.scrollToFile(path));
      if (this.urlOwned()) history?.replaceState?.(null, "", hrefOf(this.route, `#diff-${this.diffSlugs[path]}`));
    },

    /** The main side scrolled: the file under the sticky head becomes current, and the end of the loaded files reads the next page. */
    mainScrolled() {
      this.touched = true;
      if (scrollFrame || !this.showingChanges) return;
      scrollFrame = requestAnimationFrame(() => {
        scrollFrame = 0;
        this.followScroll();
      });
    },
    async followScroll() {
      const main = this.mainScroller();
      if (!main || !this.diff) return;
      if (Date.now() >= pinnedUntil) {
        // Only the sections near the viewport are measured, never every one.
        const y = main.scrollTop + 8;
        let current = "";
        let seen = false;
        for (const f of this.diff.files) {
          if (!near.has(f.path)) continue;
          seen = true;
          const el = main.querySelector(`#diff-${cssEscape(this.diffSlugs[f.path])}`);
          if (el && topIn(main, el) <= y) current = f.path;
          else if (el) break;
        }
        if (seen) this.currentFile = current || this.diff.files.find((f) => near.has(f.path)).path;
      }
      if (main.scrollTop + main.clientHeight * 2 >= main.scrollHeight) await this.loadMore();
    },
    fileScrolled() {
      this.touched = true;
    },

    /** ] and [: the next or previous file of the diff, wrapping. */
    async stepFile(delta) {
      if (!this.diff?.files.length) return;
      let files = this.diff.files;
      const i = files.findIndex((f) => f.path === this.currentFile);
      let j = i + delta;
      // Past the last loaded file: the next page, not the first file again.
      if (j >= files.length && (await this.loadMore())) files = this.diff.files;
      if (j >= files.length) {
        j = 0;
        this.say("First file again");
      } else if (j < 0) {
        j = files.length - 1;
        this.say("Last file again");
      }
      this.showMain = true;
      this.scrollToFile(files[j].path);
    },

    /** J/K in the changed files. */
    async stepSummary(delta) {
      if (!this.diff?.files.length) return;
      let files = this.diff.files;
      const i = files.findIndex((f) => f.path === this.currentFile);
      if (i + delta >= files.length && (await this.loadMore())) files = this.diff.files;
      const j = Math.max(0, Math.min(files.length - 1, i + delta));
      this.currentFile = files[j].path;
      this.$nextTick?.(() => this.$root?.querySelector?.(`.diff-summary a[href="#diff-${cssEscape(this.diffSlugs[files[j].path])}"]`)?.focus());
    },

    // ── live ─────────────────────────────────────────────────────────────
    get untouched() {
      const el = this.showingChanges ? this.mainScroller() : this.$refs?.scroll;
      return !this.touched && (!el || el.scrollTop === 0);
    },

    /** code_changed for this worktree: the side follows; the main side holds still unless untouched. */
    async codeChanged() {
      if (refreshing) {
        refreshAgain = true;
        return;
      }
      refreshing = this.refreshSide();
      try {
        await refreshing;
      } finally {
        refreshing = null;
        if (refreshAgain) {
          refreshAgain = false;
          this.codeChanged();
        }
      }
    },

    async refreshSide() {
      if (this.background) {
        // Nobody looks at the diff: the count follows, the diff loads fresh when the tab comes back.
        this.diffStale = true;
        await Promise.all([this.readTotals(), this.readWorking()]);
        return;
      }
      const decision = onChange(this.liveState, { untouched: this.untouched });
      // The changed files and their counts follow in place.
      const diff = await this.loadLoaded({ quiet: true });
      if (diff) {
        const before = new Map((this.diff?.files ?? []).map((f) => [f.path, `${f.status}:${f.added}:${f.deleted}`]));
        this.freshPaths = diff.files.filter((f) => before.get(f.path) !== `${f.status}:${f.added}:${f.deleted}`).map((f) => f.path);
        if (!this.showingChanges || decision.apply || !this.diff) this.setDiff(diff);
        else {
          this.heldDirty = new Set([...this.heldDirty, ...changedPaths(rowsOf.get(diff))]);
          this.heldDiff = diff;
          this.countHeld(diff);
        }
      }
      this.readWorking();
      // The tree's marks follow too.
      for (const dir of loadedOpenDirs(this.tree)) {
        try {
          const t = await gateway.tree(this.source, dir, this.request(`tree:${dir}`));
          this.tree = withLevel(this.tree, dir, t.entries);
        } catch (err) {
          if (codeOf(err) === Codes.WORKSPACE_MISSING) this.markGone();
        }
      }
      if (decision.apply) {
        if (this.showingFiles && this.file) await this.loadFile(this.file.path);
        if (this.showingMap) {
          this.map = null;
          await this.ensureMap();
        }
      }
      if (this.liveState !== decision.state && this.liveState !== GONE) this.liveState = decision.state;
      if (decision.announce) this.say(ANNOUNCE_CHANGED);
    },

    countHeld(diff) {
      if (this.kind === "session") this.$dispatch?.("code-files-counted", { count: diff.fileCount });
    },

    /** The uncommitted count in the source bar. */
    async readWorking() {
      if (this.kind !== "session" || this.gone || !this.source.workspaceId) return;
      try {
        const w = await gateway.diff({ workspaceId: this.source.workspaceId, scope: "working", limit: 0 }, this.request("working"));
        this.dirty = w.fileCount;
      } catch {
        // the count is a hint; the bar goes without it
      }
    },

    /** R: re-read what is on screen, keeping the place. */
    async reload() {
      if (!this.isStale) return;
      const main = this.showingChanges ? this.mainScroller() : this.$refs?.scroll;
      const top = main?.scrollTop ?? 0;
      if (this.showingChanges) {
        if (this.heldDiff) this.setDiff(this.heldDiff);
        else await this.showDiff();
        this.heldDiff = null;
      } else if (this.showingFiles && this.file) {
        await this.loadFile(this.file.path);
      } else if (this.showingMap) {
        this.map = null;
        await this.ensureMap();
      }
      this.liveState = onReload(this.liveState);
      this.$nextTick?.(() => {
        if (main) main.scrollTop = top;
        for (const c of drawn.values()) c.refresh();
        this.touched = top > 0;
      });
    },

    say(text) {
      this.announcement = text;
      this.$dispatch?.("rail-announce", text);
    },

    // ── the palette: Find file and Go to line ────────────────────────────
    async openFinder() {
      if (this.gone) return;
      this.paletteMode = "file";
      this.paletteQuery = "";
      this.paletteActive = 0;
      this.paletteNote = "";
      this.$nextTick?.(() => this.showPalette());
      const key = `${this.source.workspaceId || this.source.repositoryId}@${this.commit}`;
      if (pathsCache?.key === key && !this.source.workspaceId) {
        this.paths = pathsCache.paths;
        this.pathsTruncated = pathsCache.truncated;
        return;
      }
      try {
        const p = await gateway.paths(this.source, this.request("paths"));
        pathsCache = { key, paths: p.paths, truncated: p.truncated };
        this.paths = p.paths;
        this.pathsTruncated = p.truncated;
      } catch (err) {
        if (err?.name !== "AbortError") this.paletteNote = `${codeOf(err)} · could not list the files`;
      }
    },
    openGoToLine() {
      if (!(this.showingFiles && this.file?.content) && !(this.showingChanges && this.diff?.files.length)) return;
      this.paletteMode = "line";
      this.paletteQuery = "";
      this.paletteNote = "";
      this.$nextTick?.(() => this.showPalette());
    },
    showPalette() {
      const dialog = this.$refs?.palette;
      if (dialog && !dialog.open) dialog.showModal?.();
      this.$refs?.paletteInput?.focus();
    },
    closePalette() {
      const dialog = this.$refs?.palette;
      if (dialog?.open) dialog.close();
      this.paletteMode = "";
    },
    paletteClosed() {
      this.paletteMode = "";
    },
    paletteInput() {
      this.paletteActive = 0;
      this.paletteNote = "";
    },
    paletteMove(delta) {
      const n = this.finderRows.length;
      if (n) this.paletteActive = (this.paletteActive + delta + n) % n;
    },
    paletteChoose() {
      if (this.paletteMode === "line") {
        this.goToLine(this.paletteQuery);
        return;
      }
      const row = this.finderRows[this.paletteActive];
      if (row) this.pickPath(row.path);
    },
    pickPath(path) {
      this.closePalette();
      if (this.view !== "files") this.go({ ...this.route, view: "files", path: "" }, { push: true });
      this.enterView();
      this.openPath(path);
    },

    /** L: a line or a range, clamped, targeted, a third from the top. */
    goToLine(text) {
      const range = parseRange(text);
      if (!range) {
        this.paletteNote = "Type a line, like 120, or a range, like 120-140.";
        return;
      }
      if (this.showingChanges) {
        const f = this.diff?.files.find((x) => x.path === this.currentFile);
        const rows = rowsOf.get(this.diff)?.files.get(this.currentFile)?.rows;
        if (!f?.hasHunks || !rows || rowOfLineIn(rows, "R", range.start) < 0) {
          this.paletteNote = `Line ${range.start} is not in the diff of ${baseName(this.currentFile)}.`;
          return;
        }
        this.closePalette();
        this.target = range;
        this.targetSide = "R";
        drawn.get(`diff:${f.path}`)?.setTarget(range, "R");
        this.scrollToFile(f.path, range.start, "R");
        if (this.urlOwned()) history?.replaceState?.(null, "", hrefOf(this.route, `#diff-${this.diffSlugs[f.path]}-R${range.start}`));
        return;
      }
      const { range: clamped, note } = clampRange(range, this.file?.lineCount ?? 0);
      if (!clamped) {
        this.paletteNote = note;
        return;
      }
      this.closePalette();
      this.target = clamped;
      if (note) this.say(note);
      drawn.get("file")?.setTarget(clamped);
      this.scrollToLine(clamped.start);
      if (this.urlOwned()) history?.replaceState?.(null, "", hrefOf(this.route, rangeHash(clamped)));
    },

    /** Y: the URL pinned to the commit, copied. */
    async copyPermalink() {
      const hash = this.showingFiles && this.target ? rangeHash(this.target) : location?.hash ?? "";
      const url = new URL(permalink(this.route, this.commit, hash), location?.href ?? "http://localhost/").href;
      try {
        await copy(url);
        this.toast = this.kind === "session" ? PERMALINK_TOAST.session : PERMALINK_TOAST.repository;
      } catch {
        this.toast = "Could not copy the link.";
      }
      this.say(this.toast);
      setTimeout(() => (this.toast = ""), 4000);
    },

    // ── keys (UX §3) ─────────────────────────────────────────────────────
    onKey(event) {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey) return;
      const t = event.target;
      if (t?.closest?.("input, textarea, select, [contenteditable], dialog[open]") || this.paletteOpen) return;
      const key = event.key;
      if (sequence === "g") {
        sequence = "";
        clearTimeout(sequenceTimer);
        const view = { c: "changes", f: "files", m: "map" }[key.toLowerCase()];
        if (view && (view !== "changes" || this.kind === "session")) {
          event.preventDefault();
          this.showView(view);
        }
        return;
      }
      const run = (fn) => {
        event.preventDefault();
        fn();
      };
      switch (key) {
        case "g":
          sequence = "g";
          sequenceTimer = setTimeout(() => (sequence = ""), SEQUENCE_MS);
          return run(() => {});
        case "t":
          return run(() => this.openFinder());
        case "l":
          return run(() => this.openGoToLine());
        case "]":
          return this.showingChanges && run(() => this.stepFile(1));
        case "[":
          return this.showingChanges && run(() => this.stepFile(-1));
        case "j":
          return this.showingChanges && run(() => this.stepSummary(1));
        case "k":
          return this.showingChanges && run(() => this.stepSummary(-1));
        case "x":
          return this.showingChanges && this.currentFile && run(() => this.toggleFile(this.currentFile));
        case "X":
          return this.showingChanges && run(() => this.toggleAll());
        case "s":
          return this.showingChanges && this.canSplit && run(() => this.setLayout(this.layout === "split" ? "unified" : "split"));
        case "w":
          return this.showingFiles && run(() => this.toggleWrap());
        case "y":
          return run(() => this.copyPermalink());
        case "r":
          return this.isStale && run(() => this.reload());
        case "E":
          return this.kind === "session" && event.shiftKey && run(() => this.toggleExpand());
        case "F":
          return this.kind === "session" && event.shiftKey && run(() => this.toggleFullscreen());
        case "F6":
          return run(() => this.swapPane());
        case "Escape":
          return run(() => this.escape(t));
        default:
      }
    },

    /** F6: between the side list and the main side. */
    swapPane() {
      const side = this.$refs?.side;
      const main = this.showingChanges ? this.mainScroller() : this.$refs?.scroll ?? this.mainScroller();
      if (side?.contains(document.activeElement)) {
        this.showMain = true;
        main?.focus();
      } else {
        this.showMain = false;
        focusSide(side);
      }
    },

    /** Esc: from the code to the side list, from the side list to the views switch (or the tab bar). */
    escape(from) {
      const side = this.$refs?.side;
      if (side?.contains(from)) {
        this.leaveToTop();
        return;
      }
      this.showMain = false;
      focusSide(side);
    },
    leaveToTop() {
      this.$root?.querySelector?.('.code-source-bar nav.segmented a[aria-current="page"]')?.focus();
    },

    // ── the split between the file list and the diff ─────────────────────
    /** Inline style of the layout: the chosen width of the list, if any. */
    get splitStyle() {
      return this.splitPercent === null ? null : `--code-side: ${this.splitPercent}%`;
    },
    get splitLimits() {
      return splitBounds(this.layoutWidth || 1000, this.layoutRem);
    },
    /** aria-valuenow: the list's share of the layout, in whole percent. */
    get splitNow() {
      return Math.round(this.splitPercent ?? defaultSplit(this.layoutWidth || 1000, this.layoutRem));
    },
    get splitMin() {
      return Math.round(this.splitLimits.min);
    },
    get splitMax() {
      return Math.round(this.splitLimits.max);
    },
    get splitDraggingAttr() {
      return this.splitDragging ? "" : null;
    },
    setSplit(percent) {
      this.splitPercent = percent === null ? null : clampSplit(percent, this.layoutWidth || 1000, this.layoutRem);
    },
    storeSplit() {
      prefs.set("code.split", this.splitPercent === null ? null : String(this.splitPercent));
    },
    /** The separator's keys: arrows move it, Home gives the default back. */
    splitKey(event) {
      const next = stepSplit(this.splitPercent ?? defaultSplit(this.layoutWidth || 1000, this.layoutRem), event.key, this.layoutWidth || 1000, this.layoutRem);
      if (next === undefined) return;
      event.preventDefault();
      this.setSplit(next);
      this.storeSplit();
    },
    resetSplit() {
      this.setSplit(null);
      this.storeSplit();
    },
    startSplit(event) {
      event.preventDefault();
      event.target.setPointerCapture?.(event.pointerId);
      this.splitDragging = true;
    },
    moveSplit(event) {
      if (!this.splitDragging) return;
      const box = this.$refs.layout.getBoundingClientRect();
      if (box.width > 0) this.setSplit(((event.clientX - box.left) / box.width) * 100);
    },
    endSplit(event) {
      if (!this.splitDragging) return;
      this.splitDragging = false;
      event.target?.releasePointerCapture?.(event.pointerId);
      this.storeSplit();
    },

    /** The layout's container width decides one side or both (44rem, contract §4). */
    measure(width, rem = 16) {
      this.layoutWidth = width;
      this.layoutRem = rem;
      const narrow = width < 44 * rem;
      if (narrow !== this.narrow) {
        this.narrow = narrow;
        this.syncRows();
      }
    },
  };
  return self_;
}

/** The paths a read says changed since the read before it. */
function changedPaths(sets) {
  const out = [];
  for (const [path, f] of sets?.files ?? []) if (f.changed) out.push(path);
  return out;
}

function focusSide(side) {
  const target = side?.querySelector('.item[tabindex="0"], .diff-summary a[aria-current], .diff-summary a, .item');
  target?.focus();
}

/** An element's top inside a scroller's content, whatever lies between them. */
const topIn = (scroller, el) => el.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;

const spanTo = (range, n) => ({ start: Math.min(range.start, n), end: Math.max(range.end, n) });

const cssEscape = (s) => (globalThis.CSS?.escape ? CSS.escape(s) : String(s).replace(/["\\]/g, "\\$&"));

async function defaultCopy(text) {
  await navigator.clipboard.writeText(text);
}

/** Find file's rows: name, then the faint directory, then the change mark. */
export function finderRows(paths, query, active, diff) {
  const changes = new Map((diff?.files ?? []).map((f) => [f.path, f.status]));
  const { matches } = rank(paths, query, 100);
  return matches.map((m, i) => {
    const slash = m.path.lastIndexOf("/");
    const name = m.path.slice(slash + 1);
    const dir = slash >= 0 ? m.path.slice(0, slash) : "";
    return {
      key: m.path,
      path: m.path,
      domId: `finder-${i}`,
      active: String(i === active),
      nameParts: markParts(name, shiftRanges(m.ranges, slash + 1, name.length)).map((p, k) => ({ ...p, key: k })),
      dirParts: markParts(dir, shiftRanges(m.ranges, 0, dir.length)).map((p, k) => ({ ...p, key: k })),
      mark: changes.has(m.path) ? changeMark(changes.get(m.path)) : null,
    };
  });
}
