// The review requests the task's architect raised for the person, and the
// person's answers: approve, or request changes with a note (the architect
// passes it on). One component, two scopes:
//
//   the task page         <span x-data="reviewsInbox" data-seed="reviews-seed" data-ticket-id="t1">
//                          a Reviews button in the title row while a request waits, and its
//                          popover (task-reviews, the page's popupDialog): every waiting request;
//                          the settled ones are only counted, in the popover's foot
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
import { settledLine, toCard, waitingLine } from "../view.js";

const TICK_MS = 30_000;

/** The Reviews popover: a dialog in the top layer (the page's popupDialog). Its open state is never stored. */
const POPOVER_ID = "task-reviews";
const BUTTON_ID = "task-reviews-button";

const ANSWERED = { approved: "You approved", changes_requested: "You asked for changes on" };

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ReviewGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   hash?: () => string,
 * }} deps  hash is the page's URL fragment: a link to a review (#review-{id}) or to #reviews
 */
export const reviewsInbox = ({ gateway, clock, hash = () => globalThis.location?.hash ?? "" }) => () => {
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
    /** The Reviews popover is open (browser-only: a page loads with it closed). */
    popoverOpen: false,
    /** The page's link to a review has been followed, once the requests were read. */
    linkFollowed: false,

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
    /** The title row's Reviews button: while a request waits; until the list is read, the server's count stands. */
    get buttonShown() {
      return this.loaded ? this.pendingCount > 0 : !this.error && this.paintedPending > 0;
    },
    get buttonCount() {
      return String(this.loaded ? this.pendingCount : this.paintedPending);
    },
    get buttonName() {
      return `Reviews, ${this.buttonCount} waiting`;
    },
    /** The popover's foot: how many requests are settled on this task. */
    get settledLine() {
      return settledLine(this.settledCards.length);
    },
    get nothingWaits() {
      return this.loaded && !this.hasPending;
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
    get isEmpty() {
      return this.loaded && !this.hasPending;
    },
    get notLoaded() {
      return !this.loaded;
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
      return this.drafts[id] ?? { note: "", problem: "", busy: false, notice: "", dismissed: false, expanded: false };
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
        this.followLink();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    /**
     * The page was opened on a link to #reviews or to one review (#review-{id}):
     * open the popover and focus that review (or the popover's heading).
     */
    followLink() {
      if (this.linkFollowed || !this.forTask) return;
      this.linkFollowed = true;
      const target = hash();
      const id = target.startsWith("#review-") ? decodeURIComponent(target.slice("#review-".length)) : "";
      if ((target !== "#reviews" && !id) || !this.hasPending) return;
      this.openPopover();
      this.$nextTick(() => {
        const card = [...(root?.querySelectorAll("article.review") ?? [])].find((a) => a.id === `review-${id}`);
        const heading = card?.querySelector("[data-review-heading]") ?? root?.querySelector("[data-reviews-heading]");
        heading?.scrollIntoView?.({ block: "nearest" });
        heading?.focus?.();
      });
    },

    /** Opens the popover under the Reviews button; the dialog hands the focus back to it. */
    openPopover() {
      this.popoverOpen = true;
      this.$dispatch("dialog-open", { id: POPOVER_ID, anchor: document.getElementById(BUTTON_ID) });
    },

    closePopover() {
      this.$dispatch("dialog-dismiss", { id: POPOVER_ID });
    },

    /** The dialog closed (Esc, Close, a press outside, or the last answer). */
    popoverClosed(event) {
      if (event?.detail?.id === POPOVER_ID) this.popoverOpen = false;
    },

    ensureDraft(id) {
      if (!this.drafts[id]) this.drafts = { ...this.drafts, [id]: { note: "", problem: "", busy: false, notice: "", dismissed: false, expanded: false } };
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
    /** "Request changes…": opens the note field in the card; Send then sends it. */
    expandNote(id) {
      if (this.drafts[id]) this.drafts[id].expanded = true;
      this.$nextTick(() => document.getElementById(`review-note-${id}`)?.focus?.());
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
        if (this.popoverOpen && !this.hasPending) this.closeAfterLast();
        else this.$nextTick(() => this.focusNext());
      } catch (err) {
        this.error = describeError(err);
        if (codeOf(err) === Codes.REVIEW_NOT_PENDING || codeOf(err) === Codes.REVIEW_NOT_FOUND) this.load();
      } finally {
        draft.busy = false;
        this.answeringId = "";
      }
    },

    /** After an answer: the next request waiting, or the popover's heading when none is. */
    focusNext() {
      const target = root?.querySelector("[data-review-heading]") ?? root?.querySelector("[data-reviews-heading]");
      target?.focus?.();
    },

    /**
     * The last waiting request was answered: the popover closes, the Reviews
     * button goes with it, and the focus stays in the title row, on its first control besides it.
     */
    closeAfterLast() {
      this.closePopover();
      this.$nextTick(() => [...(root?.parentElement?.querySelectorAll("a, button") ?? [])].find((c) => !root.contains(c) && c.checkVisibility?.())?.focus?.());
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
