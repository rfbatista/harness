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
import { parseDiffHash, rowOfLine, rowOfNewLine, slugs } from "../../domain/diff.js";
import { problemOf } from "../../domain/errors.js";
import { clampRange, parseRange, parseRangeHash, rangeHash, scrollTopFor, splitLines, WRAP_UP_TO } from "../../domain/lines.js";
import { markParts, rank, shiftRanges } from "../../domain/fuzzy.js";
import { ANNOUNCE_CHANGED, FIXED, GONE, onChange, onEnded, onReload } from "../../domain/live.js";
import { PERMALINK_TOAST, hrefOf, parseHref, permalink, repositoryHref } from "../../domain/routes.js";
import { emptyTree, loadedOpenDirs, missingAncestors, openAction, revealPath, treeKey, typeAhead, visibleRows, withBusy, withExpanded, withLevel } from "../../domain/tree.js";
import { mapView, zoneIndex, zoneTags } from "../../domain/zones.js";
import { diffRows, drawLines, fileRows } from "../render/lines.js";
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

/** Above these a file is shown plain (approved limit). */
export const HIGHLIGHT_BYTES = 256 * 1024;
export const HIGHLIGHT_LINES = 10_000;
/** Split renders a file of at most this many changed lines. */
export const SPLIT_UP_TO = 3_000;
const TYPEAHEAD_MS = 500;
const SEQUENCE_MS = 1_500;

/**
 * @typedef {object} BrowserDeps
 * @property {import("../../domain/ports.js").CodeGateway} gateway
 * @property {import("../../domain/ports.js").Highlighter} highlighter
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
export function codeBrowser({ gateway, highlighter, prefs, history = globalThis.history, location = globalThis.location, copy = defaultCopy, idle, setTimeout = globalThis.setTimeout.bind(globalThis) }) {
  // Not reactive: controllers of drawn lines, in-flight requests, timers.
  const drawn = new Map();
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

  return {
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
    loadingFile: false,
    loadingMap: false,
    layout: "unified",
    wrap: false,
    narrow: false,
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
      return this.kind === "session" ? (this.diff && this.diff.files.length === 0 ? "files" : "changes") : "files";
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
    get isStale() {
      return this.liveState === "stale";
    },
    get isGone() {
      return this.liveState === GONE;
    },
    get views() {
      return viewsView(this.route, { count: this.listDiff ? this.listDiff.files.length : null, kind: this.kind, current: this.view });
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
      const fresh = new Set(this.freshPaths);
      const s = slugs(list.files.map((f) => f.path));
      return list.files.map((f) => summaryRowView(f, { slug: s.get(f.path), current: f.path === this.currentFile, fresh: fresh.has(f.path) }));
    },
    get totals() {
      return this.listDiff ? totalsView(this.listDiff) : null;
    },
    get noChanges() {
      return !!this.diff && this.diff.files.length === 0;
    },
    get noChangesText() {
      return this.scope === "working" ? "No uncommitted changes." : "No changes on this branch yet. The agent's edits appear here as it makes them.";
    },

    // ── the main side ────────────────────────────────────────────────────
    get diffFiles() {
      if (!this.diff) return [];
      const splitAllowed = this.layout === "split" && !this.narrow;
      return this.diff.files.map((f) =>
        diffFileView(f, {
          slug: this.diffSlugs[f.path],
          // The layout changes the rows: a new key draws them again.
          rev: `${this.diffRev}:${splitAllowed && f.added + f.deleted <= SPLIT_UP_TO ? "split" : "unified"}`,
          collapsed: !!this.collapsed[f.path],
          busy: !!this.busyFiles[f.path],
          asked: !!this.askedFiles[f.path],
          zones,
          diff: this.diff,
          viewHref: this.viewFileHref(f.path),
          size: undefined,
          splitAllowed: splitAllowed && (f.added + f.deleted) <= SPLIT_UP_TO,
        }),
      );
    },
    get truncatedDiff() {
      return !!this.diff?.truncated;
    },
    get canSplit() {
      return !this.narrow;
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
    /** ⇧E and the Expand button: the session list hides or comes back; the URL stays. */
    toggleExpand() {
      if (this.kind !== "session") return;
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
    openChanges(event, path) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
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
      this.layout = prefs.get("code.layout") === "split" ? "split" : "unified";
      this.wrap = prefs.get("code.wrap") === "1";
      if (seed.gone) this.markGone();
      this.baseGone = seed.baseGone;
      if (seed.map) this.setMap(seed.map);
      let tree = emptyTree();
      for (const t of seed.trees) tree = withExpanded(withLevel(tree, t.path, t.entries), t.path, t.path !== "");
      this.tree = tree;
      if (seed.trees[0]) this.commit = seed.trees[0].source.commit;
      // A repository page's ref as the server resolved it (its checkout's branch by default).
      if (this.kind === "repository" && !this.branch) this.branch = (seed.trees[0] ?? seed.file ?? seed.map)?.source.ref ?? "";
      if (seed.diff) this.setDiff(seed.diff);
      if (seed.file) this.setFile(seed.file);
      if (seed.error) this.fail(seed.error, { path: seed.route.path, ref: seed.route.ref });
      if (seed.route.view === "files" && seed.route.path && !seed.file && !seed.error) this.openPath(seed.route.path, { push: false });
      this.readHash(location?.hash ?? "");
    },

    stop() {
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
      const keep = this.diff ? { ...this.collapsed } : {};
      this.diffSlugs = Object.fromEntries(bySlug);
      this.collapsed = this.diff ? Object.fromEntries(diff.files.filter((f) => keep[f.path] ?? collapsed[f.path]).map((f) => [f.path, true])) : collapsed;
      this.diff = diff;
      this.diffRev += 1;
      this.askedFiles = {};
      if (diff.source.gone) this.markGone();
      if (!this.commit || diff.source.kind === "workspace") this.commit = diff.source.commit;
      if (!this.currentFile || !diff.files.some((f) => f.path === this.currentFile)) this.currentFile = diff.files[0]?.path ?? "";
      this.countFiles();
    },

    /** The session's tab badge follows the changed files. */
    countFiles() {
      if (this.kind === "session" && this.diff) this.$dispatch?.("code-files-counted", { count: this.diff.files.length });
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
        const diff = await gateway.diff({ workspaceId: this.source.workspaceId, scope: this.scope }, signal);
        return diff;
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

    async showDiff() {
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
      if (this.showingChanges && !this.diff) this.showDiff();
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
        this.go({ ...this.route, view: "changes", path: "", scope: this.route.scope }, { push, hash: `diff-${this.diffSlugs[path] ?? ""}` });
        if (!this.diff) await this.showDiff();
        this.scrollToFile(path);
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

    readHash(hash) {
      const diffHash = parseDiffHash(hash);
      if (diffHash) {
        const path = Object.keys(this.diffSlugs).find((p) => this.diffSlugs[p] === diffHash.slug);
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
      prefs.set("code.layout", layout === "split" ? "split" : null);
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
        const one = await gateway.diff({ ...q, path }, this.request(`diff:${path}`));
        const f = one.files[0];
        if (f) {
          this.diff = Object.freeze({ ...this.diff, files: Object.freeze(this.diff.files.map((x) => (x.path === path ? f : x))) });
          this.diffRev += 1;
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

    /** Draws a file's hunks into its .code-lines (x-init on the element). */
    drawHunks(el, path) {
      const f = this.diff?.files.find((x) => x.path === path);
      drawn.get(`diff:${path}`)?.destroy();
      if (!f?.hunks) return;
      const split = el.dataset.layout === "split";
      const ctl = drawLines(el, {
        rows: diffRows(f.hunks, split ? "split" : "unified"),
        scroller: this.mainScroller() ?? null,
        slug: this.diffSlugs[path],
        highlighter,
        language: f.language,
        virtual: !split,
        target: this.currentFile === path ? this.target : null,
        targetSide: this.targetSide,
        idle,
      });
      drawn.set(`diff:${path}`, ctl);
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
      if (!main || !slug) return;
      this.currentFile = path;
      pinnedUntil = Date.now() + 400;
      if (this.collapsed[path] && line) this.toggleFile(path);
      const section = main.querySelector?.(`#diff-${cssEscape(slug)}`);
      if (!section) return;
      let top = topIn(main, section);
      if (line) {
        const ctl = drawn.get(`diff:${path}`);
        const f = this.diff.files.find((x) => x.path === path);
        const row = rowOfLine(f?.hunks, side, line);
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

    /** The main side scrolled: the file under the sticky head becomes current. */
    mainScrolled() {
      this.touched = true;
      if (scrollFrame || !this.showingChanges || Date.now() < pinnedUntil) return;
      scrollFrame = requestAnimationFrame(() => {
        scrollFrame = 0;
        const main = this.mainScroller();
        if (!main || !this.diff) return;
        const y = main.scrollTop + 8;
        let current = this.diff.files[0]?.path ?? "";
        for (const f of this.diff.files) {
          const el = main.querySelector(`#diff-${cssEscape(this.diffSlugs[f.path])}`);
          if (el && topIn(main, el) <= y) current = f.path;
        }
        this.currentFile = current;
      });
    },
    fileScrolled() {
      this.touched = true;
    },

    /** ] and [: the next or previous file of the diff, wrapping. */
    stepFile(delta) {
      if (!this.diff?.files.length) return;
      const files = this.diff.files;
      const i = files.findIndex((f) => f.path === this.currentFile);
      let j = i + delta;
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
    stepSummary(delta) {
      if (!this.diff?.files.length) return;
      const files = this.diff.files;
      const i = files.findIndex((f) => f.path === this.currentFile);
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
      const decision = onChange(this.liveState, { untouched: this.untouched });
      // The changed files and their counts follow in place.
      const diff = await this.loadDiff({ quiet: true });
      if (diff) {
        const before = new Map((this.diff?.files ?? []).map((f) => [f.path, `${f.status}:${f.added}:${f.deleted}`]));
        this.freshPaths = diff.files.filter((f) => before.get(f.path) !== `${f.status}:${f.added}:${f.deleted}`).map((f) => f.path);
        if (!this.showingChanges || decision.apply || !this.diff) this.setDiff(diff);
        else {
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
      if (this.kind === "session") this.$dispatch?.("code-files-counted", { count: diff.files.length });
    },

    /** The uncommitted count in the source bar. */
    async readWorking() {
      if (this.kind !== "session" || this.gone || !this.source.workspaceId) return;
      try {
        const w = await gateway.diff({ workspaceId: this.source.workspaceId, scope: "working" }, this.request("working"));
        this.dirty = w.files.length;
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
        if (!f?.hunks || rowOfNewLine(f.hunks, range.start) < 0) {
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

    /** The layout's container width decides one side or both (44rem, contract §4). */
    measure(width, rem = 16) {
      const narrow = width < 44 * rem;
      if (narrow !== this.narrow) this.narrow = narrow;
    },
  };
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
