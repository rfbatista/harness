// The writing helper panel as a state machine. Every function takes a state
// and returns the next one; the Alpine component keeps the current state and
// the domain never touches the DOM or the network.
//
//   closed → start | reading
//   start --Continue--> drafting;  reading(analyze) → questions | good
//   questions --Draft / Skip--> drafting;  drafting → proposal
//   proposal --refine--> drafting;  titles: reading(titles) → titles
//   any running step → error --Try again--> that step

import { startQuestion } from "./copy.js";

/**
 * @typedef {"closed" | "start" | "reading" | "questions" | "good" | "drafting" | "proposal" | "titles" | "error"} Phase
 * @typedef {import("./ports.js").Question} Question
 *
 * @typedef {object} PanelState
 * @property {Phase} phase
 * @property {import("./ports.js").FieldKind} kind
 * @property {"analyze" | "draft" | "titles"} step  the step that runs or last ran
 * @property {Question[]} questions
 * @property {Record<string, string>} answers   by question id
 * @property {string} intent                    the start question's answer
 * @property {string} draft                     the proposal, or what streamed so far
 * @property {string} baseline                  the field's text when the draft began
 * @property {string} request                   the refine request of the running draft
 * @property {boolean} stale                    the field changed after the draft began
 * @property {"changes" | "clean"} view
 * @property {string[]} titles                  the options on show
 * @property {string[]} offered                 every title offered so far
 * @property {{ code: string, message: string, next: string } | null} error
 * @property {"analyze" | "draft" | "titles"} retry  what Try again runs
 * @property {{ phase: Phase, draft: string } | null} from  where a draft step began
 */

export const MAX_QUESTIONS = 3;

/** @returns {PanelState} */
export function closed() {
  return {
    phase: "closed",
    kind: "task_description",
    step: "analyze",
    questions: [],
    answers: {},
    intent: "",
    draft: "",
    baseline: "",
    request: "",
    stale: false,
    view: "changes",
    titles: [],
    offered: [],
    error: null,
    retry: "analyze",
    from: null,
  };
}

/** The panel opens on a field: a title reads its options, anything else asks, or reads what is there. */
export function opened({ kind, text }) {
  const base = { ...closed(), kind };
  if (kind === "task_title") return { ...base, phase: "reading", step: "titles" };
  if (text.trim() === "") return { ...base, phase: "start" };
  return { ...base, phase: "reading", step: "analyze" };
}

/** A step is about to run again from this state (Three more, Try again after a read). */
export function reading(s, step) {
  return { ...s, phase: "reading", step, error: null };
}

export function analyzed(s, result) {
  const questions = (result.questions ?? []).slice(0, MAX_QUESTIONS);
  if (result.already_good || questions.length === 0) return { ...s, phase: "good", questions: [], error: null };
  return { ...s, phase: "questions", questions, answers: {}, error: null };
}

export function titled(s, result) {
  const titles = result.titles ?? [];
  return { ...s, phase: "titles", titles, offered: [...s.offered, ...titles], error: null };
}

export const setAnswer = (s, id, text) => ({ ...s, answers: { ...s.answers, [id]: text } });

/** A chip is an answer; picking the one already picked clears it. */
export function pickChip(s, id, chip) {
  return setAnswer(s, id, s.answers[id] === chip ? "" : chip);
}

export const chipPressed = (s, id, chip) => s.answers[id] === chip;

export const setIntent = (s, text) => ({ ...s, intent: text });

export const setView = (s, view) => ({ ...s, view });

const filled = (text) => (text ?? "").trim() !== "";

export function answeredCount(s) {
  return s.questions.filter((q) => filled(s.answers[q.id])).length;
}

/** The answers a draft step sends, in question order; an unanswered question is left out. */
export function answersFor(s) {
  if (s.phase === "start" || (s.questions.length === 0 && filled(s.intent))) {
    return filled(s.intent) ? [{ question: startQuestion(s.kind), answer: s.intent.trim() }] : [];
  }
  return s.questions.filter((q) => filled(s.answers[q.id])).map((q) => ({ question: q.text, answer: s.answers[q.id].trim() }));
}

export const canContinue = (s) => filled(s.intent);

/** A draft (or a refine of one) starts: the field's text is the baseline staleness is measured from. */
export function drafting(s, { baseline, request = "" }) {
  // Stop goes back to where the person was; a retry from an error keeps the place the failed draft began.
  const keep = s.phase === "drafting" || s.phase === "error" ? s.from : { phase: s.phase, draft: s.draft };
  return { ...s, phase: "drafting", step: "draft", draft: "", baseline, request, stale: false, from: keep, error: null };
}

export const delta = (s, text) => ({ ...s, draft: s.draft + text });

export const drafted = (s, text) => ({ ...s, phase: "proposal", draft: text, from: null });

/** The person typed in the field: a draft made from older text is stale. */
export function fieldChanged(s, text) {
  if (s.phase !== "drafting" && s.phase !== "proposal") return s;
  const stale = text !== s.baseline;
  return stale === s.stale ? s : { ...s, stale };
}

/** Stop: back to where the step began; a read that stops closes the panel. */
export function stopped(s) {
  if (s.phase === "reading") return closed();
  if (s.phase !== "drafting") return s;
  const from = s.from ?? { phase: "start", draft: "" };
  return { ...s, phase: from.phase, draft: from.draft, stale: false, from: null };
}

export function failed(s, error) {
  const retry = s.phase === "reading" ? s.step : s.phase === "drafting" ? "draft" : s.retry;
  return { ...s, phase: "error", error, retry };
}

/** The open placeholders a draft still has. */
export const openGaps = (text) => text.match(/\[open:[^\]]*\]/g) ?? [];
