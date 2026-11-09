// Sending or dismissing an open interview, as the footer lives it: what the
// Send and Dismiss buttons lead through, whether the person is asked first,
// and what a failure leaves. No I/O, no Alpine, no DOM.
//
//   idle ─send─▶ confirming(send) ─confirm─▶ sending(send) ─done─▶ idle (the card left the open ones)
//     │  ╲ no blanks ─────────────────────────▶   │ failed ─▶ failed ─retry─▶ sending
//     └─dismiss─▶ confirming(dismiss) ─confirm─▶ sending(dismiss)
//
// A second send of the same interview while it sends changes nothing: the one
// in flight is the send. A retry after a failure sends again with no second
// question, because the server answers a repeat of the same send with the
// stored result and delivers nothing twice.

import { Codes } from "../../../shared/domain/errors.js";

/**
 * @typedef {"idle"|"confirming"|"sending"|"failed"} Phase
 * @typedef {{ code: string, message: string, retry: boolean, closed: import("./interviews.js").Interview | null }} Failure
 *          `closed`: the interview as the server says it is now, when the refusal carries it
 * @typedef {{ phase: Phase, kind: "send"|"dismiss"|null, error: Failure | null }} Step
 */

/** @type {Step} */
export const idle = Object.freeze({ phase: "idle", kind: null, error: null });

const step = (phase, kind, error = null) => Object.freeze({ phase, kind, error });

/**
 * @param {Step} state
 * @param {{ type: "send", skipped: number } | { type: "confirm" | "review" | "dismiss" | "keep" | "retry" | "done" } | { type: "failed", error: Failure }} action
 * @returns {Step}
 */
export function reduce(state, action) {
  if (state.phase === "sending") {
    if (action.type === "done") return idle;
    if (action.type === "failed") return step("failed", state.kind, action.error);
    return state;
  }
  switch (action.type) {
    case "send":
      if (state.phase === "confirming" && state.kind === "send") return step("sending", "send");
      return action.skipped > 0 ? step("confirming", "send") : step("sending", "send");
    case "confirm":
      return state.phase === "confirming" ? step("sending", state.kind) : state;
    case "review":
    case "keep":
      return state.phase === "confirming" ? idle : state;
    case "dismiss":
      return step("confirming", "dismiss");
    case "retry":
      return state.phase === "failed" && state.error?.retry ? step("sending", state.kind) : state;
    default:
      return state;
  }
}

/** Only a live stream may send: nothing goes into the void. A resync is followed by live. */
export const canSend = (connection) => connection === "live" || connection === "resynced";

/** The words under Send while the stream is not live. */
export const WAITING_FOR_CONNECTION = "Waiting for the connection";

const CODED = new Set(["INTERVIEW_EXPIRED", "INTERVIEW_NOT_OPEN", "INTERVIEW_ANSWERS_INVALID", "INTERVIEW_NOT_FOUND"]);

/**
 * What a refused or failed answer means for the person. The server's coded
 * refusals are shown as they are (the expired and not-open ones carry the
 * interview as it is now); anything else, a network drop or a server error,
 * is the UI's own INTERVIEW_SEND_FAILED: the harness did not answer, try again.
 * The gateway has read the carried interview into the model (`details.interview`).
 * @returns {Failure}
 */
export function failureOf(err) {
  const code = err?.code;
  if (code === Codes.NETWORK || !CODED.has(code)) return { code: "INTERVIEW_SEND_FAILED", message: "", retry: true, closed: null };
  return { code, message: String(err.message ?? ""), retry: false, closed: err.details?.interview ?? null };
}
