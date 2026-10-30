// The session page's Files tab: a session's virtual file system as a tree of
// two roots (worktree, resources) whose folders load a page at a time, with a
// read-only viewer beside it. The viewer is the Code tab's: this component
// composes codeBrowser for the file, the split and the lines, and replaces
// its tree. Nothing here creates, renames, deletes or uploads a file.
//
//   <template x-for="panel in filesPanels" x-bind:key="panel.key">
//     <section x-data="filesPanel(panel)" …>…</section>
//   </template>
//
// The tab follows the page's shared stream (deps.files.follow), never its
// own: session status moves the strip, session_files_root the roots.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { codeOf } from "../../../../shared/domain/errors.js";
import { agentWords, emptyFilesTree, filesRows, levelKey, newLevel, openUnder } from "../../domain/files.js";
import { parseSplit } from "../../domain/split.js";
import { crumbsView } from "../view.js";
import { codeBrowser } from "./codeBrowser.js";
import { compose } from "./compose.js";

/** The rows drawn at first, and added each frame while more wait. */
export const ROWS_CHUNK = 80;

/** The session's Files-tab preferences, kept in this browser only. */
export const SPLIT_PREF = "files.split";

/**
 * @typedef {object} FilesPanelInput  what the sessions page passes
 * @property {string} key
 * @property {string} projectId
 * @property {string} taskId
 * @property {string} sessionId
 * @property {string} workspaceId
 * @property {string} title
 * @property {string} branch
 * @property {boolean} shown  the Files tab is in front now
 */

/** @param {import("./codeBrowser.js").BrowserDeps & { files: ReturnType<import("../../infrastructure/files-gateway.js").filesGateway> }} deps */
export const filesPanel = (deps) => (/** @type {FilesPanelInput} */ panel) => {
  const { files, prefs } = deps;
  const browser = codeBrowser(deps);
  let unfollow = () => {};
  let observer = null;
  let waiting = new Set();
  let listed = null;
  let views = new Map();
  let lastStatus = "";
  return compose(browser, {
    kind: "session",
    sessionId: "",
    title: "",
    shown: false,
    wide: true,
    /** The Files tab's own tree; codeBrowser's `tree` stays unused. */
    ftree: emptyFilesTree(),
    /** How many rows of the tree are drawn; it grows by ROWS_CHUNK a frame. */
    drawn: ROWS_CHUNK,
    agentState: "working",
    loadedRoots: false,
    rootsProblem: null,
    selectedKey: "",
    focusKey: "",
    viewerExpanded: false,

    init() {
      this.sessionId = panel.sessionId;
      this.title = panel.title ?? "";
      this.shown = !!panel.shown;
      this.startFrom(
        deps.gateway.decodeSeed({
          project_id: panel.projectId,
          route: { kind: "session", project_id: panel.projectId, task_id: panel.taskId, session_id: panel.sessionId, view: "files" },
          source: { workspace_id: panel.workspaceId },
          branch: panel.branch,
        }),
      );
      this.branch = panel.branch ?? "";
      // The Files tab keeps its own width; the Code tab's is not this one's.
      this.splitPercent = parseSplit(prefs.get(SPLIT_PREF));
      this.watchWidth();
      unfollow = files.follow(panel.projectId, {
        onRoot: (c) => c.sessionId === this.sessionId && this.setRoot(c.root, c.available, c.note),
        onState: (s) => {
          if (s.sessionId === this.sessionId) this.agentState = s.agentState;
        },
        onStatus: (status) => {
          // A stream that came back may have missed changes: read the roots again.
          if (status === FeedStatus.RESYNCED || (status === FeedStatus.LIVE && lastStatus === FeedStatus.PAUSED)) this.loadRoots();
          lastStatus = status;
        },
      });
      this.loadRoots();
    },

    destroy() {
      observer?.disconnect();
      unfollow();
      this.stop();
    },

    // ── what the strip and the tree show ─────────────────────────────────
    get strip() {
      return agentWords(this.agentState);
    },
    get stripWaiting() {
      return this.agentState === "waiting";
    },
    /** Every row of the tree, as the domain lists them, until the tree changes. */
    get allRows() {
      const tree = this.ftree;
      if (listed?.tree !== tree) listed = { tree, rows: filesRows(tree) };
      return listed.rows;
    },
    /**
     * The rows drawn: a page of a big folder is not drawn at once, so no
     * frame is a long task. A row whose state did not change is the same
     * object as before, so the list leaves it alone.
     */
    get rows() {
      const out = [];
      const next = new Map();
      for (const r of this.allRows.slice(0, this.drawn)) {
        const sig = this.rowSig(r);
        const known = views.get(r.key);
        const view = known?.sig === sig ? known.view : this.rowView(r);
        next.set(r.key, { sig, view });
        out.push(view);
      }
      views = next;
      return out;
    },
    /** What a row's view depends on, besides the row itself. */
    rowSig(r) {
      return `${r.open}|${r.state}|${r.text ?? ""}|${r.root?.available ?? ""}|${r.root?.note ?? ""}|${r.type === "entry" && this.selectedKey === `${r.root}:${r.path}`}|${this.tabindexOf(r.key)}|${this.branch}`;
    },
    /** x-effect: draws the next chunk of rows in the next frame while some wait. */
    growRows() {
      if (this.drawn >= this.allRows.length) return;
      (deps.frame ?? ((cb) => requestAnimationFrame(cb)))(() => {
        this.drawn += ROWS_CHUNK;
      });
    },
    get treeBusy() {
      return this.loadedRoots ? null : "true";
    },
    get viewerExpandedAttr() {
      return this.viewerExpanded ? "" : null;
    },
    get expandLabel() {
      return this.viewerExpanded ? "Show the tree" : "Expand the viewer";
    },
    get expandPressed() {
      return String(this.viewerExpanded);
    },
    get filesProblemLine() {
      return !!this.rootsProblem;
    },
    get emptyProblem() {
      return null;
    },
    /** The viewer's crumbs: the root, then each folder and the file, none a link. */
    get fileCrumbs() {
      const path = this.file?.path ?? "";
      return crumbsView(path, { rootName: this.openRootLabel, dirHref: () => null });
    },
    get openRootLabel() {
      return this.ftree.roots.find((r) => r.root === this.openRoot)?.label ?? "Worktree";
    },
    openRoot: "worktree",

    rowView(r) {
      const base = { key: r.key, type: r.type, depth: r.depth, depthStyle: `--depth:${r.depth}`, level: r.depth + 1 };
      switch (r.type) {
        case "root": {
          const gone = !r.root.available;
          return {
            ...base,
            role: "treeitem",
            isRoot: true,
            isEntry: false,
            gone,
            name: r.root.label,
            meta: gone ? "" : r.root.root === "worktree" ? (this.branch ?? "") : "task and project files",
            kind: "root",
            expanded: gone ? null : String(r.open),
            isDir: !gone,
            disabled: gone ? "true" : null,
            tag: gone ? "unavailable" : "",
            label: r.root.label,
            title: r.root.label,
            root: r.root.root,
            dir: "",
            path: "",
            levelKey: r.level,
            selected: null,
            tabindex: this.tabindexOf(r.key),
          };
        }
        case "entry": {
          const e = r.entry;
          const isDir = e.kind === "dir";
          return {
            ...base,
            role: "treeitem",
            isRoot: false,
            isEntry: true,
            isDir,
            isFile: e.kind === "file",
            isLink: e.kind === "symlink",
            gone: false,
            name: e.name,
            target: e.target,
            kind: e.kind,
            ignoredAttr: e.ignored ? "" : null,
            tag: e.ignored ? "ignored" : "",
            expanded: isDir ? String(r.open) : null,
            selected: this.selectedKey === `${r.root}:${r.path}` ? "true" : null,
            label: e.name,
            title: e.ignored ? `${r.path} (ignored by git)` : r.path,
            root: r.root,
            dir: r.dir,
            path: r.path,
            levelKey: levelKey(r.root, r.path),
            setsize: r.size,
            posinset: r.pos,
            tabindex: this.tabindexOf(r.key),
          };
        }
        case "more":
          return { ...base, role: "none", root: r.root, dir: r.dir, state: r.state, loading: r.state === "loading" ? "true" : null, failed: r.state === "error", text: r.text, levelKey: r.level };
        case "note":
          return { ...base, role: "none", text: r.text };
        default:
          return { ...base, role: "none" };
      }
    },
    tabindexOf(key) {
      const first = this.focusKey || this.firstRowKey();
      return key === first ? "0" : "-1";
    },
    firstRowKey() {
      return this.ftree.roots[0] ? `root:${this.ftree.roots[0].root}` : "";
    },

    // ── reading ──────────────────────────────────────────────────────────
    async loadRoots() {
      try {
        const answer = await files.roots(this.sessionId, this.request("roots"));
        this.rootsProblem = null;
        this.agentState = answer.agentState;
        this.ftree = { ...this.ftree, roots: [...answer.roots] };
        this.loadedRoots = true;
        // The worktree opens by itself, the way the mockup shows it.
        const wt = answer.roots.find((r) => r.root === "worktree" && r.available);
        if (wt && this.ftree.expanded[levelKey("worktree", "")] === undefined) this.expand("worktree", "");
      } catch (err) {
        if (err?.name === "AbortError") return;
        this.loadedRoots = true;
        this.rootsProblem = { text: err?.message ?? "The files could not be read.", code: codeOf(err) };
      }
    },

    setLevel(key, level) {
      this.ftree = { ...this.ftree, levels: { ...this.ftree.levels, [key]: level } };
    },
    setExpanded(key, open) {
      const expanded = { ...this.ftree.expanded };
      if (open) expanded[key] = true;
      else delete expanded[key];
      this.ftree = { ...this.ftree, expanded };
    },

    /** Opens a folder (or root) and reads its first page when it has none. */
    expand(root, dir) {
      const key = levelKey(root, dir);
      this.setExpanded(key, true);
      if (!this.ftree.levels[key]) return this.loadLevel(root, dir, false);
    },
    collapse(root, dir) {
      this.setExpanded(levelKey(root, dir), false);
    },

    /** One page of a folder: the first, or with more the one after the last entry shown. */
    async loadLevel(root, dir, more) {
      const key = levelKey(root, dir);
      const cur = this.ftree.levels[key] ?? newLevel();
      if (cur.loading) return;
      this.setLevel(key, { ...cur, loading: true, error: "" });
      try {
        const page = await files.entries(this.sessionId, { root, dir, after: more ? cur.next : "" }, this.request(`entries:${key}`));
        this.setLevel(key, { entries: more ? [...cur.entries, ...page.entries] : [...page.entries], next: page.next, loading: false, error: "" });
      } catch (err) {
        if (err?.name === "AbortError") return;
        if (codeOf(err) === "ROOT_GONE") {
          this.setRoot(root, false, err.message);
          return;
        }
        this.setLevel(key, { ...cur, loading: false, error: err?.message ?? "Could not load the entries." });
      }
    },

    /** The paging row came into view: read the next page once. */
    watchMore(el, root, dir) {
      if (typeof IntersectionObserver !== "function") return;
      const key = levelKey(root, dir);
      if (waiting.has(el)) return;
      waiting.add(el);
      const io = new IntersectionObserver((hits) => {
        if (!hits.some((h) => h.isIntersecting)) return;
        io.disconnect();
        waiting.delete(el);
        const level = this.ftree.levels[key];
        if (level && level.next && !level.loading && !level.error) this.loadLevel(root, dir, true);
      }, { root: el.closest(".side"), rootMargin: "0px 0px 200px 0px" });
      io.observe(el);
    },
    retryMore(root, dir) {
      const level = this.ftree.levels[levelKey(root, dir)];
      this.loadLevel(root, dir, !!level && level.entries.length > 0);
    },

    // ── the roots coming and going ───────────────────────────────────────
    setRoot(root, available, note) {
      const roots = this.ftree.roots.map((r) => (r.root === root ? Object.freeze({ ...r, available, note: available ? "" : note }) : r));
      let { levels, expanded } = this.ftree;
      if (!available) {
        levels = { ...levels };
        expanded = { ...expanded };
        for (const k of openUnder(this.ftree, root)) delete expanded[k];
        for (const k of Object.keys(levels)) if (k.startsWith(`${root}:`)) delete levels[k];
      }
      this.ftree = { roots, levels, expanded };
    },

    // ── the person's moves ───────────────────────────────────────────────
    rowOf(key) {
      const r = this.allRows.find((row) => row.key === key);
      return r ? this.rowView(r) : undefined;
    },
    clickRow(event, key) {
      event.preventDefault();
      const row = this.rowOf(key);
      if (row) this.activate(row);
    },
    activate(row) {
      this.focusKey = row.key;
      if (row.type === "root") {
        if (row.gone) return;
        if (row.expanded === "true") this.collapse(row.root, "");
        else this.expand(row.root, "");
        return;
      }
      if (row.type !== "entry") return;
      if (row.isDir) {
        if (row.expanded === "true") this.collapse(row.root, row.path);
        else this.expand(row.root, row.path);
      } else if (row.isFile) this.openFile(row.root, row.path);
    },
    async openFile(root, path) {
      this.selectedKey = `${root}:${path}`;
      this.openRoot = root;
      this.problem = null;
      this.showMain = true;
      this.loadingFile = true;
      try {
        this.setFile(await files.file(this.sessionId, root, path, this.request("file")));
      } catch (err) {
        if (err?.name === "AbortError") return;
        this.file = null;
        this.fail(err, { path });
      } finally {
        this.loadingFile = false;
      }
    },

    /** The tree's keys (WAI-ARIA tree): arrows move, Right and Left open and close, Enter opens. */
    treeKeydown(event) {
      const rows = this.rows.filter((r) => r.type === "root" || r.type === "entry");
      const i = rows.findIndex((r) => r.key === (this.focusKey || this.firstRowKey()));
      const at = rows[i];
      let to = null;
      switch (event.key) {
        case "ArrowDown":
          to = rows[Math.min(rows.length - 1, i + 1)];
          break;
        case "ArrowUp":
          to = rows[Math.max(0, i - 1)];
          break;
        case "Home":
          to = rows[0];
          break;
        case "End":
          to = rows[rows.length - 1];
          break;
        case "ArrowRight":
          if (at?.isDir && at.expanded === "false") this.activate(at);
          else to = rows[i + 1];
          break;
        case "ArrowLeft":
          if (at?.isDir && at.expanded === "true") this.activate(at);
          break;
        case "Enter":
        case " ":
          if (at) this.activate(at);
          break;
        default:
          return;
      }
      event.preventDefault();
      if (to) {
        this.focusKey = to.key;
        this.$nextTick?.(() => this.$root?.querySelector?.(`[data-key="${CSS.escape(to.key)}"] > .item`)?.focus());
      }
    },

    toggleViewer() {
      this.viewerExpanded = !this.viewerExpanded;
    },
    openAgentTab() {
      this.$dispatch("files-open-agent", {});
    },

    // The Files tab keeps its width apart from the Code tab's.
    storeSplit() {
      prefs.set(SPLIT_PREF, this.splitPercent === null ? null : String(this.splitPercent));
    },
    urlOwned() {
      return false;
    },
    diffHas() {
      return false;
    },

    watchWidth() {
      const el = this.$el;
      if (!el || typeof ResizeObserver !== "function") return;
      const rem = parseFloat(getComputedStyle(el.ownerDocument.documentElement).fontSize) || 16;
      observer = new ResizeObserver((entries) => {
        const width = entries[0].contentRect.width;
        if (width > 0) this.measure(width, rem);
      });
      observer.observe(el);
    },
  });
};

