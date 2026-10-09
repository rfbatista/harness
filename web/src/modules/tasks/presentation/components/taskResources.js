// A task's resources and comments: the "Resources and comments" sub-page.
// Resources (uploads and links) on one side, the comment thread and its
// composer on the other. The page starts from the seed the server rendered,
// writes through the ResourceGateway and follows the project feed, so what
// another tab or person adds, renames or deletes shows here live. The echo
// of the person's own add is held until its POST answers (domain/items.js),
// so nothing shows twice. Sessions write comments too; theirs are never an
// echo. The ids of the sessions listed on the task, from the seed and then
// the feed, say which of them a comment can link to.
//
//   <script id="task-resources-seed" type="application/json">{"project_id", "ticket_id", "resources": [...], "comments": [...], "session_ids": [...]}</script>
//   <main x-data="tasksResources" data-seed="task-resources-seed"> …
//
// The parts (adding, lightbox, thread) share this object: they read the
// lists and call toast, announce and the show/forget helpers below.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { count } from "../../../../shared/presentation/format.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { authorName, isBySession } from "../../domain/comments.js";
import { commentSignature, echoGuard, hasId, removeById, resourceSignature, upsert } from "../../domain/items.js";
import { titleProblem } from "../../domain/resources.js";
import { resourceRow, summaryWords } from "../resourceView.js";
import { commentThread } from "./commentThread.js";
import { resourceAdding } from "./resourceAdding.js";
import { resourceLightbox } from "./resourceLightbox.js";

/** How long a toast stays. */
export const TOAST_MS = 6_000;
/** How often the relative times move on. */
export const TICK_MS = 60_000;

export function compose(...parts) {
  const out = {};
  for (const part of parts) Object.defineProperties(out, Object.getOwnPropertyDescriptors(part));
  return out;
}

/**
 * @typedef {object} ResourcesDeps
 * @property {import("../../domain/ports.js").ResourceGateway} resources
 * @property {import("../../../../shared/infrastructure/clock.js").Clock} clock
 * @property {(text: string) => Promise<void>} [copy]  writes to the clipboard
 * @property {typeof globalThis.setTimeout} [setTimeout]
 * @property {typeof globalThis.setInterval} [setInterval]
 * @property {typeof globalThis.clearInterval} [clearInterval]
 *
 * @param {ResourcesDeps} deps
 */
export const taskResources = (deps) => () => {
  const {
    resources: gateway,
    clock,
    copy = (text) => globalThis.navigator.clipboard.writeText(text),
    setTimeout = globalThis.setTimeout.bind(globalThis),
    setInterval = globalThis.setInterval.bind(globalThis),
    clearInterval = globalThis.clearInterval.bind(globalThis),
  } = deps;
  const guards = { resources: echoGuard(resourceSignature), comments: echoGuard(commentSignature) };
  let unfollow = () => {};
  let ticker = null;
  let nextToast = 1;
  /** The page's <main>. A handler on an element that x-if then removes loses $root, so the page keeps its own. */
  let root = null;
  /** Changes the feed delivers while a resync's lists are on their way: applied again on top of them. */
  let duringResync = null;
  const parts = { gateway, clock, guards, setTimeout };

  return compose(resourceAdding(parts), resourceLightbox(), commentThread(parts), {
    projectId: "",
    ticketId: "",
    /** @type {import("../../domain/resources.js").Resource[]} */
    resources: [],
    /** @type {import("../../domain/comments.js").Comment[]} */
    comments: [],
    /** The sessions listed on the task. */
    sessionIds: [],
    /** Ids that arrived after the page loaded: they take the arrival wash once. */
    freshIds: [],
    now: clock.now(),
    /** The task was deleted: nothing can be added any more. */
    taskGone: false,
    /** @type {{ id: number, message: string, tone: string }[]} */
    toasts: [],
    announcement: "",

    renamingId: "",
    renameText: "",
    renameError: "",
    confirmingId: "",
    /** The resource a rename or delete is in flight for. */
    busyId: "",

    init() {
      root = this.$el;
      const seed = gateway.decodeSeed(readSeed(this.$el));
      this.projectId = seed.projectId;
      this.ticketId = seed.ticketId;
      this.resources = seed.resources;
      this.comments = seed.comments;
      this.sessionIds = seed.sessionIds;
      unfollow = gateway.follow(this.projectId, (change) => this.apply(change), (status) => status === FeedStatus.RESYNCED && this.resync());
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
      this.initAdding?.();
      this.initThread?.();
    },

    destroy() {
      unfollow();
      if (ticker !== null) clearInterval(ticker);
      this.destroyAdding?.();
    },

    // ── what the markup binds ──────────────────────────────────────────
    get rows() {
      const fresh = new Set(this.freshIds);
      return this.resources.map((r) =>
        resourceRow(r, { now: this.now, fresh: fresh.has(r.id), renaming: r.id === this.renamingId, confirming: r.id === this.confirmingId, busy: r.id === this.busyId }),
      );
    },
    get resourceCount() {
      return this.resources.length;
    },
    get commentCount() {
      return this.comments.length;
    },
    get resourceCountLabel() {
      return count(this.resources.length, "resource");
    },
    get commentCountLabel() {
      return count(this.comments.length, "comment");
    },
    get summary() {
      return summaryWords(this.resources.length, this.comments.length);
    },
    get projectHref() {
      return `/projects/${encodeURIComponent(this.projectId)}`;
    },
    get taskHref() {
      return `${this.projectHref}/tasks/${encodeURIComponent(this.ticketId)}`;
    },

    // ── the lists ──────────────────────────────────────────────────────
    /** Shows a resource: in place when known, else at the end, washed when fresh. */
    showResource(r, fresh = true) {
      const known = hasId(this.resources, r.id);
      this.resources = upsert(this.resources, r);
      if (!known && fresh) this.freshIds = [...this.freshIds, r.id];
    },
    forgetResource(id) {
      this.resources = removeById(this.resources, id);
      if (this.renamingId === id) this.renamingId = "";
      if (this.confirmingId === id) this.confirmingId = "";
    },
    showComment(c, fresh = true) {
      const known = hasId(this.comments, c.id);
      this.comments = upsert(this.comments, c);
      if (!known && fresh) this.freshIds = [...this.freshIds, c.id];
    },
    forgetComment(id) {
      this.comments = removeById(this.comments, id);
      if (this.editingId === id) this.editingId = "";
      if (this.confirmingCommentId === id) this.confirmingCommentId = "";
    },

    /** @param {import("../../domain/ports.js").ResourceChange} change */
    apply(change) {
      duringResync?.push(change);
      switch (change.kind) {
        case "task-deleted":
          if (change.id === this.ticketId) this.loseTask();
          return;
        case "resource": {
          const r = change.resource;
          if (r.ticketId !== this.ticketId) return;
          if (hasId(this.resources, r.id) || !guards.resources.hold(r)) this.showResource(r);
          return;
        }
        case "resource-deleted": {
          if (change.ticketId !== this.ticketId) return;
          guards.resources.drop(change.id);
          const r = this.resources.find((x) => x.id === change.id);
          if (!r) return;
          // The echo of the person's own delete (in flight: busyId) is not news.
          const elsewhere = this.busyId !== r.id && (this.renamingId === r.id || this.confirmingId === r.id);
          if (elsewhere) this.toast(`“${r.title}” was deleted elsewhere.`);
          if (this.lightboxId === r.id) this.lightboxGone();
          this.forgetResource(r.id);
          return;
        }
        case "comment": {
          const c = change.comment;
          if (c.ticketId !== this.ticketId) return;
          // Only the person's own add can be an echo; a session's is news.
          if (isBySession(c)) {
            if (!hasId(this.comments, c.id)) this.announce(`${authorName(c)} commented.`);
            this.showComment(c);
            return;
          }
          if (hasId(this.comments, c.id) || !guards.comments.hold(c)) this.showComment(c);
          return;
        }
        case "session":
          if (change.ticketId === this.ticketId && !this.sessionIds.includes(change.id)) this.sessionIds = [...this.sessionIds, change.id];
          return;
        case "session-deleted":
          this.sessionIds = this.sessionIds.filter((id) => id !== change.id);
          return;
        case "comment-deleted": {
          if (change.ticketId !== this.ticketId) return;
          guards.comments.drop(change.id);
          if (!hasId(this.comments, change.id)) return;
          const elsewhere = this.busyCommentId !== change.id && (this.editingId === change.id || this.confirmingCommentId === change.id);
          if (elsewhere) this.toast("That comment was deleted elsewhere.");
          this.forgetComment(change.id);
          return;
        }
      }
    },

    /** The feed dropped and came back: changes were missed, so the lists are read again. */
    async resync() {
      const seen = [];
      duringResync = seen;
      try {
        const [resources, comments] = await Promise.all([gateway.listResources(this.ticketId), gateway.listComments(this.ticketId)]);
        duringResync = null;
        this.resources = resources;
        this.comments = comments;
        // What arrived while the lists were on their way is newer than them.
        for (const change of seen) this.apply(change);
        if (this.renamingId && !hasId(this.resources, this.renamingId)) this.renamingId = "";
        if (this.confirmingId && !hasId(this.resources, this.confirmingId)) this.confirmingId = "";
        if (this.editingId && !hasId(this.comments, this.editingId)) this.editingId = "";
        if (this.confirmingCommentId && !hasId(this.comments, this.confirmingCommentId)) this.confirmingCommentId = "";
        if (this.lightboxId && !hasId(this.resources, this.lightboxId)) this.closeLightbox();
      } catch (err) {
        duringResync = null;
        if (codeOf(err) === Codes.TICKET_NOT_FOUND) this.loseTask();
        // otherwise keep what is shown; the next resync reads again
      }
    },

    loseTask() {
      this.taskGone = true;
      this.renamingId = "";
      this.confirmingId = "";
      this.editingId = "";
      this.confirmingCommentId = "";
      this.linkOpen = false;
      if (this.lightboxId) this.closeLightbox();
      this.cancelUploads?.();
    },

    // ── feedback ───────────────────────────────────────────────────────
    toast(message, tone = "") {
      const id = nextToast++;
      this.toasts = [...this.toasts, { id, message, tone }];
      setTimeout(() => this.dismissToast(id), TOAST_MS);
    },
    dismissToast(id) {
      this.toasts = this.toasts.filter((t) => t.id !== id);
    },
    /** Says it to screen readers through the polite live region. */
    announce(message) {
      // The same words twice in a row must still be read out.
      this.announcement = this.announcement === message ? `${message} ` : message;
    },

    /** The page's root element. */
    pageRoot() {
      return root;
    },

    /** Focuses the first element matching `selector` in the page, after Alpine renders. */
    focusLater(selector) {
      this.$nextTick(() => {
        const el = root?.querySelector(selector);
        el?.focus();
        if (el instanceof HTMLInputElement && el.dataset.select !== undefined) el.select();
      });
    },

    // ── a resource's row: menu, rename, delete ─────────────────────────
    /** The row menu ran an item: menu-select from the shared menu, inside the row. */
    rowAction(event) {
      const id = event.target?.closest?.("[data-id]")?.dataset.id ?? "";
      const r = this.resources.find((x) => x.id === id);
      if (!r) return;
      switch (event.detail?.action) {
        case "view":
          this.openLightbox(id, event.target.closest(".menu")?.querySelector(":scope > button"));
          return;
        case "copy-address":
          copy(r.url).then(
            () => this.toast("Address copied."),
            () => this.toast("The address could not be copied: select it from the link instead.", "danger"),
          );
          return;
        case "rename":
          this.startRename(id);
          return;
        case "delete":
          this.askDelete(id);
      }
    },

    startRename(id) {
      const r = this.resources.find((x) => x.id === id);
      if (!r || this.taskGone) return;
      this.confirmingId = "";
      this.renamingId = id;
      this.renameText = r.title;
      this.renameError = "";
      this.focusLater(`[data-id="${CSS.escape(id)}"] [data-rename]`);
    },
    cancelRename() {
      const id = this.renamingId;
      this.renamingId = "";
      this.renameError = "";
      this.focusLater(`[data-id="${CSS.escape(id)}"] .menu > button`);
    },
    async saveRename() {
      const id = this.renamingId;
      const r = this.resources.find((x) => x.id === id);
      if (!r || this.busyId) return;
      const problem = titleProblem(this.renameText);
      if (problem) {
        this.renameError = problem;
        return;
      }
      const title = this.renameText.trim();
      if (title === r.title) return this.cancelRename();
      this.busyId = id;
      try {
        const saved = await gateway.retitle(id, title);
        this.showResource(saved, false);
        this.announce(`Renamed to “${saved.title}”.`);
        this.cancelRename();
      } catch (err) {
        if (codeOf(err) === Codes.RESOURCE_NOT_FOUND) {
          this.forgetResource(id);
          this.toast(`“${r.title}” was already deleted.`);
        } else {
          this.renameError = codeOf(err) === Codes.INVALID_INPUT ? err.message : `The title was not saved: ${err.message}`;
        }
      } finally {
        this.busyId = "";
      }
    },

    askDelete(id) {
      if (!hasId(this.resources, id)) return;
      this.renamingId = "";
      this.confirmingId = id;
      this.focusLater(`[data-id="${CSS.escape(id)}"] [data-keep]`);
    },
    keep() {
      const id = this.confirmingId;
      this.confirmingId = "";
      this.focusLater(`[data-id="${CSS.escape(id)}"] .menu > button`);
    },
    async confirmDelete() {
      const id = this.confirmingId;
      const r = this.resources.find((x) => x.id === id);
      if (!r || this.busyId) return;
      this.busyId = id;
      try {
        await gateway.removeResource(id);
      } catch (err) {
        // Already gone (the feed may have removed it first) is what the person asked for.
        if (codeOf(err) !== Codes.RESOURCE_NOT_FOUND) {
          this.busyId = "";
          this.toast(`“${r.title}” was not deleted: ${err.message}`, "danger");
          return;
        }
      }
      this.busyId = "";
      this.forgetResource(id);
      this.toast(`Deleted “${r.title}”.`);
      this.focusLater("[data-add-files]");
    },
  });
};
