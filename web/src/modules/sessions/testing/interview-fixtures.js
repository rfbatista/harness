// Interviews as the server sends them (snake_case, RFC 3339), and the model
// the web UI reads them into.

import { toInterview } from "../infrastructure/interviews-dto.js";

/** One question on the wire: single choice, the second option recommended. */
export const questionDTO = (over = {}) => ({
  title: "Which store holds interviews?",
  body: "Pick **one**.\n\n- the database\n- a file",
  kind: "single",
  options: [
    { label: "A file per interview", description: "" },
    { label: "The harness database", description: "It survives restarts." },
  ],
  recommended: { option: 1 },
  ...over,
});

/** An open interview of two questions on the wire. */
export const interviewDTO = (over = {}) => ({
  id: "iv-1",
  session_id: "s1",
  agent_name: "architect",
  title: "",
  asked_at: "2026-11-08T14:32:00Z",
  state: "open",
  ended_at: null,
  delivery: null,
  delivered_at: null,
  questions: [questionDTO(), questionDTO({ title: "Where is it read?", body: "", kind: "text", options: [], recommended: null })],
  answers: null,
  dismiss_note: null,
  ...over,
});

export const makeInterview = (over = {}) => toInterview(interviewDTO(over));

/** A session's list at seq, holding these interviews (in the model). */
export const makeList = (seq, ...interviews) => ({ seq, interviews });
