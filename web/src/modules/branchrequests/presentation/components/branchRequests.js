// The branch requests agents of the task wait on the person for, and the
// person's answers: Approve or Deny, with a note the agent gets (ADR 0069).
//
//   <span x-data="branchRequests" data-seed="branch-requests-seed" data-ticket-id="t1" data-pending="1">
//     a Branch requests button in the task page's title row while a request waits,
//     and its popover (task-branch-requests, the page's popupDialog)
//
// Requests arrive, and are answered elsewhere, over the project feed. The
// person's own answer echoes on the feed too, possibly before its POST answers;
// the request in flight is not announced as someone else's.

import { codeOf, Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { isPending, oldestPendingBySession, upsertRequest } from "../../domain/request.js";
import { toCard, waitingLine } from "../view.js";

const TICK_MS = 30_000;
const POPOVER_ID = "task-branch-requests";
const BUTTON_ID = "task-branch-requests-button";

/**
 * @param {{
 *   gateway: ReturnType<typeof import("../../infrastructure/branch-requests-gateway.js").branchRequestsGateway>,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   store: { pendingBySession: Record<string, string> },
 *   hash?: () => string,
 * }} deps  store is what the session rows read to lead to a request; hash is the page's URL fragment: a link to a request (#branch-request-{id}) or to #branch-requests
 */
export const branchRequests = ({ gateway, clock, store, hash = () => globalThis.location?.hash ?? "" }) => () => {
  let unfollow = () => {};
  let ticker = null;
  let root = null;

  return {
    projectId: "",
    ticketId: "",
    sessions: {},
    /** @type {import("../../domain/request.js").BranchRequest[]} */
    requests: [],
    loaded: false,
    error: null,
    announcement: "",
    now: clock.now(),
    /** The note written per request, and whether its answer is in flight. */
    drafts: {},
    /** How many requests the server painted as waiting (data-pending), until the list is read. */
    paintedPending: 0,
    answeringId: "",
    popoverOpen: false,
    linkFollowed: false,

    // ── what the markup binds ────────────────────────────────────────────
    get pendingCards() {
      return this.requests.filter(isPending).map((r) => ({ ...toCard(r, { sessions: this.sessions, now: this.now }), draft: this.draftOf(r.id), noteId: `branch-request-note-${r.id}` }));
    },
    get pendingCount() {
      return this.requests.filter(isPending).length;
    },
    get buttonShown() {
      return this.loaded ? this.pendingCount > 0 : !this.error && this.paintedPending > 0;
    },
    get buttonCount() {
      return String(this.loaded ? this.pendingCount : this.paintedPending);
    },
    get buttonName() {
      return `Branch requests, ${this.buttonCount} waiting`;
    },
    get headline() {
      return waitingLine(this.pendingCount);
    },

    draftOf(id) {
      return this.drafts[id] ?? { note: "", busy: false };
    },
    ensureDraft(id) {
      if (!this.drafts[id]) this.drafts = { ...this.drafts, [id]: { note: "", busy: false } };
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      root = this.$el;
      try {
        const seed = readSeed(this.$el) ?? {};
        this.projectId = seed.project_id ?? "";
        this.ticketId = this.$el.dataset.ticketId ?? "";
        this.paintedPending = Number(this.$el.dataset.pending ?? 0);
        this.sessions = { ...seed.sessions };
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      this.$watch("requests", () => (store.pendingBySession = oldestPendingBySession(this.requests)));
      this.load();
      unfollow = gateway.follow(
        this.projectId,
        (event) => this.arrived(event.request),
        (status) => status === FeedStatus.RESYNCED && this.load(),
      );
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
    },

    destroy() {
      unfollow();
      clearInterval(ticker);
    },

    async load() {
      try {
        const list = await gateway.listPendingForTask(this.ticketId);
        this.requests = list.reduce(upsertRequest, []);
        for (const r of this.requests) this.ensureDraft(r.id);
        this.loaded = true;
        this.now = clock.now();
        this.followLink();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    /** The page was opened on #branch-requests or on one request (#branch-request-{id}): open the popover and focus it. */
    followLink() {
      if (this.linkFollowed) return;
      this.linkFollowed = true;
      const target = hash();
      const id = target.startsWith("#branch-request-") ? decodeURIComponent(target.slice("#branch-request-".length)) : "";
      if ((target !== "#branch-requests" && !id) || this.pendingCount === 0) return;
      this.showRequest(id);
    },

    /** A session row asked for its pending approval: open at that session's oldest pending request. */
    showFor(event) {
      const id = store.pendingBySession[event?.detail?.sessionId];
      if (id) this.showRequest(id);
    },

    /** Opens the popover and focuses the request's card (the list's heading when id is ""). */
    showRequest(id) {
      this.openPopover();
      this.$nextTick(() => {
        const card = [...(root?.querySelectorAll("article[data-branch-request]") ?? [])].find((a) => a.dataset.requestId === id);
        const heading = card?.querySelector("[data-branch-request-heading]") ?? root?.querySelector("[data-branch-requests-heading]");
        heading?.scrollIntoView?.({ block: "nearest" });
        heading?.focus?.();
      });
    },

    /**
     * Tells the page, from the component's root: a handler's $dispatch goes from the
     * element that fired it, and a card's button is gone by the time an answer returns.
     */
    emit(name, detail) {
      root?.dispatchEvent(new CustomEvent(name, { detail, bubbles: true }));
    },

    openPopover() {
      this.popoverOpen = true;
      this.emit("dialog-open", { id: POPOVER_ID, anchor: document.getElementById(BUTTON_ID) });
    },

    closePopover() {
      this.emit("dialog-dismiss", { id: POPOVER_ID });
    },

    popoverClosed(event) {
      if (event?.detail?.id === POPOVER_ID) this.popoverOpen = false;
    },

    /** A request filed or changed, on the feed. */
    arrived(request) {
      if (request.taskId !== this.ticketId) return;
      const before = this.requests.find((r) => r.id === request.id);
      this.ensureDraft(request.id);
      this.requests = upsertRequest(this.requests, request);
      if (request.id === this.answeringId) return; // the person's own answer
      if (!before && isPending(request)) this.say(`An agent asks to ${request.action} a branch: ${request.branch || request.startPoint?.ref || ""}`);
      else if (before && isPending(before) && !isPending(request)) this.say("A branch request was answered elsewhere");
    },

    // ── the person's answer ──────────────────────────────────────────────
    approve(id) {
      return this.answer(id, "approve");
    },
    deny(id) {
      return this.answer(id, "deny");
    },

    async answer(id, decision) {
      const draft = this.drafts[id];
      if (!draft || draft.busy) return;
      draft.busy = true;
      this.answeringId = id;
      this.error = null;
      try {
        const answered = await gateway.answer({ id, decision, note: draft.note.trim() });
        this.requests = upsertRequest(this.requests, answered);
        this.say(decision === "approve" ? "You approved the branch request" : "You denied the branch request");
        if (this.popoverOpen && this.pendingCount === 0) this.closePopover();
      } catch (err) {
        this.error = describeError(err);
        if (codeOf(err) === Codes.BRANCH_REQUEST_ANSWERED || codeOf(err) === Codes.BRANCH_REQUEST_NOT_FOUND) this.load();
      } finally {
        draft.busy = false;
        this.answeringId = "";
      }
    },

    say(text) {
      this.announcement = text;
      this.emit("announce", { text });
    },

    dismissError() {
      this.error = null;
    },
  };
};
