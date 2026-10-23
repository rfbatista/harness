// A session's Merge tab: merge its branch into the repository's default
// branch, in the local checkout, which the harness server does (ADR 0021).
//
//   <template x-for="panel in mergePanels" x-bind:key="panel.key">
//     <section x-data="sessionsMerge(panel)"> …
//
// The panel is keyed on the session and on its newest merge result, so the
// session's change on the stream (which carries the recorded result) mounts a
// fresh one. It asks whether a merge applies when it mounts. The button is
// disabled while a merge runs; the result shows from the answer at once and
// from the session after that.

import { describeError } from "../../../../shared/presentation/errors.js";
import { mergeLabel, resultView, unavailableText } from "../mergeView.js";

/** @param {{ merge: import("../../domain/ports.js").MergeGateway }} deps */
export const merge = ({ merge: gateway }) => (panel = {}) => {
  let gone = false;
  const aborter = new AbortController();

  return {
    sessionId: panel.sessionId ?? "",
    branch: panel.branch ?? "",
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
      return this.offer !== null && !this.offer.available;
    },
    get unavailable() {
      return unavailableText(this.offer);
    },
    get label() {
      return mergeLabel(this.offer);
    },
    get into() {
      return this.offer?.into || "the default branch";
    },
    get cannotMerge() {
      return this.running || !this.offered;
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
    async load() {
      try {
        const offer = await gateway.offer(this.sessionId, aborter.signal);
        if (!gone) this.offer = offer;
      } catch (err) {
        if (!gone && err?.name !== "AbortError") this.error = describeError(err).message;
      }
    },
    async mergeNow() {
      if (this.cannotMerge) return;
      this.running = true;
      this.error = "";
      try {
        this.last = await gateway.merge(this.sessionId);
      } catch (err) {
        // A refusal is on the session too; until the stream says so, say it from the answer.
        this.last = Object.freeze({ at: new Date(), merged: false, into: this.offer?.into ?? "", head: "", code: err?.code ?? "", message: err?.message ?? String(err) });
      } finally {
        this.running = false;
      }
    },
  };
};
