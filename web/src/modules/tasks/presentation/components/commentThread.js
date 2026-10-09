// The comment thread and its composer, a part of the task's resources page
// (taskResources.js). Comments are plain text, oldest first, edited and
// deleted in place. Each new one reaches the sessions running on the task as
// a message; edits do not, and the page says so.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { canSend, counterTone, counterWords, isUnchanged, MAX_COMMENT, WARN_FROM } from "../../domain/comments.js";
import { commentClaim } from "../../domain/items.js";
import { runeCount } from "../../domain/resources.js";
import { commentRow } from "../resourceView.js";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ResourceGateway,
 *   guards: { comments: ReturnType<typeof import("../../domain/items.js").echoGuard> },
 * }} deps
 */
export function commentThread({ gateway, guards }) {
  return {
    draft: "",
    sending: false,
    composerError: "",

    editingId: "",
    editText: "",
    editError: "",
    confirmingCommentId: "",
    busyCommentId: "",

    initThread() {
      if (globalThis.location?.hash === "#comments") this.focusLater("[data-composer]");
    },

    get commentRows() {
      const fresh = new Set(this.freshIds);
      return this.comments.map((c) =>
        commentRow(c, {
          now: this.now,
          fresh: fresh.has(c.id),
          editing: c.id === this.editingId,
          confirming: c.id === this.confirmingCommentId,
          busy: c.id === this.busyCommentId,
        }),
      );
    },
    get hasNoComments() {
      return this.comments.length === 0;
    },

    // ── the composer ───────────────────────────────────────────────────
    get draftLength() {
      // As the server counts: runes, after the trim.
      return runeCount(this.draft.trim());
    },
    get cannotSend() {
      return this.sending || this.taskGone || !canSend(this.draft);
    },
    get showsCounter() {
      return this.draftLength >= WARN_FROM;
    },
    get counter() {
      return counterWords(this.draftLength);
    },
    get counterTone() {
      return counterTone(this.draftLength);
    },

    async send() {
      if (this.cannotSend) return;
      const body = this.draft;
      this.sending = true;
      this.composerError = "";
      const claim = guards.comments.claim(commentClaim(body));
      try {
        const comment = await gateway.addComment(this.ticketId, body);
        for (const twin of guards.comments.settle(claim, comment.id)) this.showComment(twin);
        this.showComment(comment);
        if (this.draft === body) this.draft = "";
        this.announce("Comment added. Sessions running on this task get it as a message.");
      } catch (err) {
        for (const held of guards.comments.settle(claim, "")) this.showComment(held);
        if (codeOf(err) === Codes.TICKET_NOT_FOUND) this.loseTask();
        this.composerError = `The comment was not added: ${err?.message ?? "something went wrong"}.`;
      } finally {
        this.sending = false;
      }
    },

    // ── edit ───────────────────────────────────────────────────────────
    startEdit(id) {
      const c = this.comments.find((x) => x.id === id);
      if (!c || this.taskGone) return;
      this.confirmingCommentId = "";
      this.editingId = id;
      this.editText = c.body;
      this.editError = "";
      this.focusLater(`[data-comment-id="${CSS.escape(id)}"] textarea`);
    },
    cancelEdit() {
      const id = this.editingId;
      this.editingId = "";
      this.editError = "";
      this.focusLater(`[data-comment-id="${CSS.escape(id)}"] [data-edit]`);
    },
    async saveEdit() {
      const id = this.editingId;
      const c = this.comments.find((x) => x.id === id);
      if (!c || this.busyCommentId || this.taskGone) return;
      if (isUnchanged(c, this.editText)) return this.cancelEdit();
      if (!canSend(this.editText)) {
        this.editError = this.editText.trim() ? `Keep it to ${MAX_COMMENT.toLocaleString("en-US")} characters.` : "A comment needs some text; delete it instead.";
        return;
      }
      this.busyCommentId = id;
      try {
        this.showComment(await gateway.editComment(id, this.editText), false);
        this.announce("Comment saved.");
        this.cancelEdit();
      } catch (err) {
        if (codeOf(err) === Codes.COMMENT_NOT_FOUND) {
          this.forgetComment(id);
          this.toast("That comment was already deleted.");
        } else {
          this.editError = `The edit was not saved: ${err?.message ?? "something went wrong"}.`;
        }
      } finally {
        this.busyCommentId = "";
      }
    },

    // ── delete ─────────────────────────────────────────────────────────
    askDeleteComment(id) {
      if (this.taskGone || !this.comments.some((x) => x.id === id)) return;
      this.editingId = "";
      this.confirmingCommentId = id;
      this.focusLater(`[data-comment-id="${CSS.escape(id)}"] [data-keep]`);
    },
    keepComment() {
      const id = this.confirmingCommentId;
      this.confirmingCommentId = "";
      this.focusLater(`[data-comment-id="${CSS.escape(id)}"] [data-edit]`);
    },
    async confirmDeleteComment() {
      const id = this.confirmingCommentId;
      if (!id || this.busyCommentId) return;
      this.busyCommentId = id;
      try {
        await gateway.removeComment(id);
      } catch (err) {
        if (codeOf(err) !== Codes.COMMENT_NOT_FOUND) {
          this.busyCommentId = "";
          this.toast(`The comment was not deleted: ${err?.message ?? "something went wrong"}.`, "danger");
          return;
        }
      }
      this.busyCommentId = "";
      this.forgetComment(id);
      this.announce("Comment deleted.");
      this.focusLater("[data-composer]");
    },
  };
}
