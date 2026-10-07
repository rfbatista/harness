// The review requests the task's architect raised for the person, and the
// person's answers: approve, or request changes with a note (the architect
// passes it on). One component, two scopes:
//
//   the task page's band   <section x-data="reviewsInbox" data-seed="reviews-seed" data-ticket-id="t1">
//                          pending first, the settled ones under "Earlier reviews"
//   the project's inbox    <main x-data="reviewsInbox" data-seed="reviews-seed">
//                          every pending request of the project, by task
//
// Requests arrive, are withdrawn and are answered elsewhere over the project
// feed. The person's own answer echoes on the feed too, possibly before its
// POST answers; the request in flight is not announced as someone else's.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { isPending, noteProblem, upsertReview } from "../../domain/review.js";
import { toCard, waitingLine } from "../view.js";

const TICK_MS = 30_000;

const ANSWERED = { approved: "You approved", changes_requested: "You asked for changes on" };

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ReviewGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const reviewsInbox = ({ gateway, clock }) => () => {
  let unfollow = () => {};
  let ticker = null;
  /** The component's root: inside a handler $el is the element that fired. */
  let root = null;

  return {
    projectId: "",
    /** The task page's band; "" for the project's inbox. */
    ticketId: "",
    /** @type {import("../view.js").Names} */
    names: { sessions: {}, tasks: {}, documents: {} },
    /** @type {import("../../domain/review.js").ReviewRequest[]} */
    reviews: [],
    loaded: false,
    error: null,
    announcement: "",
    now: clock.now(),
    /** The note being written per request, and what happened to its answer. */
    drafts: {},
    /** How many requests the server painted as waiting (data-pending), until the list is read. */
    paintedPending: 0,
    /** The task has an architect (data-architect): it may have requests, settled ones included. */
    hasArchitect: false,
    /** The request whose answer is in flight: its echo is not news. */
    answeringId: "",

    // ── what the markup binds ────────────────────────────────────────────
    get forTask() {
      return this.ticketId !== "";
    },
    /**
     * Pending requests, newest first, with the ones the architect withdrew
     * while the person was writing a note: those stay, read-only, until dismissed.
     */
    get pendingCards() {
      return this.reviews
        .filter((r) => isPending(r) || this.withdrawnWhileWriting(r))
        .map((r, i) => ({ ...this.card(r), approveVariant: i === 0 ? "primary" : null }));
    },
    /** On the task page: what was answered or withdrawn, newest first. */
    get settledCards() {
      if (!this.forTask) return [];
      return this.reviews.filter((r) => !isPending(r) && !this.withdrawnWhileWriting(r)).map((r) => this.card(r));
    },
    get pendingCount() {
      return this.reviews.filter(isPending).length;
    },
    get headline() {
      return waitingLine(this.pendingCount);
    },
    get hasPending() {
      return this.pendingCards.length > 0;
    },
    get hasSettled() {
      return this.settledCards.length > 0;
    },
    get settledLabel() {
      return `Earlier reviews (${this.settledCards.length})`;
    },
    get isEmpty() {
      return this.loaded && !this.hasPending;
    },
    get notLoaded() {
      return !this.loaded;
    },
    /** The task's band hides when there is nothing to show; until loaded, the server's paint stands. */
    get hidesBand() {
      if (!this.loaded) return this.paintedPending === 0;
      return !this.hasPending && !this.hasSettled;
    },
    /** What x-bind sets on the band: amber while requests wait; null leaves it off. */
    get attentionAttr() {
      return this.hasPending ? "" : null;
    },
    /** The project's inbox: pending requests by task, in the order their newest arrived. */
    get groups() {
      const byTask = new Map();
      for (const c of this.pendingCards) {
        if (!byTask.has(c.taskId)) {
          const task = this.names.tasks[c.taskId];
          byTask.set(c.taskId, { taskId: c.taskId, title: task?.title ?? "A task", href: task?.href ?? "", cards: [] });
        }
        byTask.get(c.taskId).cards.push(c);
      }
      return [...byTask.values()];
    },

    card(r) {
      const draft = this.draftOf(r.id);
      return {
        ...toCard(r, { names: this.names, projectId: this.projectId, now: this.now }),
        draft,
        withdrawn: r.state === "withdrawn",
        // What the field's attributes bind: ids per request, and the error state.
        noteId: `review-note-${r.id}`,
        errorId: `review-note-error-${r.id}`,
        invalid: draft.problem ? "true" : null,
      };
    },
    draftOf(id) {
      return this.drafts[id] ?? { note: "", problem: "", busy: false, notice: "", dismissed: false };
    },
    withdrawnWhileWriting(r) {
      const d = this.drafts[r.id];
      return r.state === "withdrawn" && !!d && d.note.trim() !== "" && !d.dismissed;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      root = this.$el;
      try {
        const seed = readSeed(this.$el) ?? {};
        this.projectId = seed.project_id ?? "";
        this.ticketId = this.$el.dataset.ticketId ?? "";
        this.paintedPending = Number(this.$el.dataset.pending ?? 0);
        this.hasArchitect = this.$el.dataset.architect !== undefined;
        this.names = { sessions: { ...seed.sessions }, tasks: { ...seed.tasks }, documents: { ...seed.documents } };
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      // A task that never had an architect has no requests to read: any it
      // gets from now on arrive over the feed.
      if (!this.forTask || this.hasArchitect || this.paintedPending > 0) this.load();
      else this.loaded = true;
      unfollow = gateway.follow(
        this.projectId,
        (event) => this.arrived(event.review),
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
        const list = this.forTask ? await gateway.listForTask(this.ticketId) : await gateway.listPendingForProject(this.projectId);
        // Keep what the person is writing on a request that was withdrawn meanwhile.
        const kept = this.reviews.filter((r) => this.withdrawnWhileWriting(r) && !list.some((x) => x.id === r.id));
        this.reviews = [...list, ...kept].reduce(upsertReview, []);
        for (const r of this.reviews) this.ensureDraft(r.id);
        this.loaded = true;
        this.now = clock.now();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    ensureDraft(id) {
      if (!this.drafts[id]) this.drafts = { ...this.drafts, [id]: { note: "", problem: "", busy: false, notice: "", dismissed: false } };
    },

    /** A request raised or changed, on the feed. */
    arrived(review) {
      if (this.forTask ? review.taskId !== this.ticketId : review.projectId !== this.projectId) return;
      const before = this.reviews.find((r) => r.id === review.id);
      this.ensureDraft(review.id);
      this.reviews = upsertReview(this.reviews, review);
      if (review.id === this.answeringId) return; // the person's own answer
      if (!before && isPending(review)) this.say(`The architect asks for your review: ${review.subject}`);
      else if (before && isPending(before) && review.state === "withdrawn") this.say(`The architect withdrew “${review.subject}”`);
      else if (before && isPending(before) && !isPending(review)) this.say(`“${review.subject}” was answered elsewhere`);
    },

    // ── the person's answer ──────────────────────────────────────────────
    approve(id) {
      return this.answer(id, "approved");
    },
    requestChanges(id) {
      return this.answer(id, "changes_requested");
    },

    async answer(id, decision) {
      const draft = this.drafts[id];
      const review = this.reviews.find((r) => r.id === id);
      if (!draft || !review || draft.busy) return;
      draft.problem = noteProblem(decision, draft.note);
      if (draft.problem) return;
      draft.busy = true;
      this.answeringId = id;
      this.error = null;
      try {
        const { review: answered, delivered } = await gateway.respond({ reviewId: id, decision, note: draft.note.trim() });
        this.reviews = upsertReview(this.reviews, answered);
        draft.notice = delivered ? "" : "Saved. The architect gets your answer when its current turn ends.";
        this.say(`${ANSWERED[decision]} “${review.subject}”`);
        this.$nextTick(() => this.focusNext());
      } catch (err) {
        this.error = describeError(err);
        if (codeOf(err) === Codes.REVIEW_NOT_PENDING || codeOf(err) === Codes.REVIEW_NOT_FOUND) this.load();
      } finally {
        draft.busy = false;
        this.answeringId = "";
      }
    },

    /** After an answer: the next request waiting, or the band's heading when none is. */
    focusNext() {
      const target = root?.querySelector("[data-review-heading]") ?? root?.querySelector("[data-reviews-heading]");
      target?.focus?.();
    },

    /** Hides a withdrawn request the person was answering. */
    dismiss(id) {
      if (this.drafts[id]) this.drafts[id].dismissed = true;
    },

    /** Says it in the page's polite live region (the task page's, or the inbox's own). */
    say(text) {
      this.announcement = text;
      this.$dispatch("announce", { text });
    },

    dismissError() {
      this.error = null;
    },
  };
};
