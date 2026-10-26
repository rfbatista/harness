// A session's Merge tab: merge its branch into a target the person picks
// among the repository's local branches, in the local checkout, which the
// harness server does (ADR 0021, ADR 0035).
//
//   <template x-for="panel in mergePanels" x-bind:key="panel.key">
//     <section x-data="sessionsMerge(panel)"> …
//
// The panel is keyed on the session and on its newest merge result, so the
// session's change on the stream (which carries the recorded result) mounts a
// fresh one. It asks whether a merge applies when it mounts. The button is
// disabled while a merge runs; the result shows from the answer at once and
// from the session after that. The person picks the target and the method;
// the offer is asked again for each target.

import { describeError } from "../../../../shared/presentation/errors.js";
import { blockedText, consequence, isBlocked, mergeLabel, METHODS, resultView, targetLabel, uncommittedText, unavailableText } from "../mergeView.js";

/** More branches than this get a filter in the Into picker. */
const FILTER_ABOVE = 8;

/** @param {{ merge: import("../../domain/ports.js").MergeGateway }} deps */
export const merge = ({ merge: gateway }) => (panel = {}) => {
  let gone = false;
  const aborter = new AbortController();

  return {
    sessionId: panel.sessionId ?? "",
    branch: panel.branch ?? "",
    /** The target picked; "" until the first offer names the default branch. */
    target: panel.target ?? "",
    method: "merge",
    /** The squash commit message: the offer's suggestion until the person edits it. */
    message: "",
    messageEdited: false,
    /** An offer for another target is on its way; the form keeps the old one meanwhile. */
    refreshing: false,
    asked: 0,
    /** @type {import("../../domain/ports.js").MergeOffer | null} */
    offer: null,
    /** @type {import("../../domain/session.js").MergeResult | null} */
    last: panel.last ?? null,
    running: false,
    /** A failure that left no result on the session: the server did not answer. */
    error: "",

    // ── what the markup binds ────────────────────────────────────────────
    get asking() {
      return this.offer === null && this.error === "";
    },
    get offered() {
      return this.offer?.available === true;
    },
    get notOffered() {
      return this.offer !== null && !this.offer.available && !isBlocked(this.offer);
    },
    /** The target is checked out and dirty: the button shows, disabled, with this beside it. */
    get blocked() {
      return blockedText(this.offer);
    },
    get warning() {
      return uncommittedText(this.offer);
    },
    get methods() {
      return METHODS;
    },
    get squash() {
      return this.method === "squash";
    },
    get targetOptions() {
      return (this.offer?.targets ?? []).map((t) => ({ name: t.name, label: targetLabel(t) }));
    },
    /** A filter appears once the list is long. */
    get filterable() {
      return (this.offer?.targets.length ?? 0) > FILTER_ABOVE;
    },
    get plain() {
      return !this.filterable;
    },
    get consequence() {
      return consequence(this.offer, this.method);
    },
    /** The picker and the form wait while the answer for the new target is on its way. */
    get formBusy() {
      return this.running || this.refreshing;
    },
    get unavailable() {
      return unavailableText(this.offer);
    },
    get label() {
      return mergeLabel(this.offer, this.method);
    },
    get into() {
      return this.offer?.into || "the default branch";
    },
    /** Squash needs a message: the commit has to say something. */
    get messageMissing() {
      return this.squash && this.message.trim() === "";
    },
    get cannotMerge() {
      return this.formBusy || !this.offered || this.messageMissing;
    },
    /** Why the button is disabled, in words beside it; "" when it is not (or only busy). */
    get buttonReason() {
      if (this.blocked) return this.blocked;
      return this.offered && this.messageMissing ? "Write the commit message for the squash." : "";
    },
    /** The button shows when a merge is offered, or is blocked in a way the person can fix. */
    get showButton() {
      return this.offered || this.blocked !== "";
    },
    get busyAttr() {
      return this.running ? "true" : null;
    },
    get result() {
      return resultView(this.last);
    },
    get hasResult() {
      return this.result !== null;
    },
    get resultTitle() {
      return this.result?.title ?? "";
    },
    get resultDetail() {
      return this.result?.detail ?? "";
    },
    get resultNote() {
      return this.result?.note ?? "";
    },
    get resultRole() {
      return this.result?.role ?? "status";
    },
    get resultCode() {
      return this.result?.code ?? "";
    },
    get resultTone() {
      return this.result?.tone ?? null;
    },

    // ── actions ──────────────────────────────────────────────────────────
    init() {
      return this.load();
    },
    destroy() {
      gone = true;
      aborter.abort();
    },
    /** Asks what a merge into the picked target would do; an answer to an older question is dropped. */
    async load() {
      const asked = ++this.asked;
      this.refreshing = true;
      try {
        const offer = await gateway.offer(this.sessionId, this.target, aborter.signal);
        if (gone || asked !== this.asked) return;
        this.offer = offer;
        this.error = "";
        // The first offer names the default branch; a later one confirms the pick.
        this.target = offer.into || this.target;
        if (!this.messageEdited) this.message = offer.suggestedMessage;
      } catch (err) {
        if (!gone && asked === this.asked && err?.name !== "AbortError") this.error = describeError(err).message;
      } finally {
        if (asked === this.asked) this.refreshing = false;
      }
    },
    /** The person picked another target. */
    targetChanged() {
      return this.load();
    },
    messageTyped() {
      this.messageEdited = true;
    },
    async mergeNow() {
      if (this.cannotMerge) return;
      this.running = true;
      this.error = "";
      try {
        this.last = await gateway.merge(this.sessionId, { target: this.target, method: this.method, message: this.message.trim() });
      } catch (err) {
        // A refusal is on the session too; until the stream says so, say it from the answer.
        this.last = Object.freeze({
          at: new Date(),
          merged: false,
          method: this.method,
          into: this.target || this.offer?.into || "",
          head: "",
          code: err?.code ?? "",
          message: err?.message ?? String(err),
          files: Object.freeze(err?.details?.files ?? []),
        });
      } finally {
        this.running = false;
      }
    },
  };
};
