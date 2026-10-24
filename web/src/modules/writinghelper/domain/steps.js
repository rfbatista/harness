// The body of each step request. The browser holds the conversation, so every
// request carries all it needs: the field, its siblings, the answers, the
// current draft.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** The server's limit for the field, a sibling, the draft and the request. */
export const MAX_TEXT = 20_000;
const MAX_SIBLINGS = 4;

const tooLong = (what) =>
  new StructuredError(Codes.INVALID_INPUT, `The ${what} is longer than the helper reads (20,000 characters). Shorten it first.`);

/**
 * @param {"analyze" | "draft" | "titles"} step
 * @param {{
 *   projectId: string,
 *   kind: import("./ports.js").FieldKind,
 *   text: string,
 *   siblings?: { label: string, text: string }[],
 *   answers?: { question: string, answer: string }[],
 *   draft?: string,
 *   request?: string,
 *   exclude?: string[],
 * }} input
 * @returns {import("./ports.js").StepRequest}
 */
export function stepRequest(step, { projectId, kind, text, siblings = [], answers = [], draft = "", request = "", exclude = [] }) {
  if (text.length > MAX_TEXT) throw tooLong("text");
  // A sibling is context: a long one is cut, where the field itself is refused.
  const read = siblings
    .filter((s) => s.text.trim() !== "")
    .slice(0, MAX_SIBLINGS)
    .map((s) => (s.text.length > MAX_TEXT ? { ...s, text: s.text.slice(0, MAX_TEXT) } : s));
  if (draft.length > MAX_TEXT) throw tooLong("draft");
  if (request.length > MAX_TEXT) throw tooLong("request");

  /** @type {import("./ports.js").StepRequest} */
  const body = { project_id: projectId, step, field: { kind, text } };
  if (read.length) body.siblings = read;
  if (step === "draft") {
    if (answers.length) body.answers = answers;
    if (draft) body.draft = draft;
    if (request) body.request = request;
  }
  if (step === "titles" && exclude.length) body.exclude = exclude;
  return body;
}
