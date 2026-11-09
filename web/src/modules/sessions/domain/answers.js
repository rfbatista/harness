// What the person has answered in an open interview, per question, and the
// rules that count it. Mirrors the contract's Answer {chosen[], text, note,
// skipped}; no I/O, no Alpine, no DOM.
//
// A question's state is { chosen, own, text, note }: the options ticked (by
// their index in the interview), whether "Write my own" is selected, the typed
// text (the answer of a text question, or the own answer of a choice), and the
// note. A blank question is a skipped one: nothing is preselected.

/**
 * @typedef {{ chosen: readonly number[], own: boolean, text: string, note: string }} AnswerState
 * @typedef {import("./interviews.js").Question} Question
 */

/** @returns {AnswerState} */
export const blankAnswer = () => ({ chosen: [], own: false, text: "", note: "" });

/** @param {readonly Question[]} questions @returns {AnswerState[]} */
export const blankAnswers = (questions) => questions.map(blankAnswer);

const typed = (text) => text.trim() !== "";

/** Whether the question holds an answer. A note is never one by itself. */
export function isAnswered(question, state) {
  if (question.kind === "text") return typed(state.text);
  return state.chosen.length > 0 || (state.own && typed(state.text));
}

/**
 * Ticks or unticks option `index`. A single choice holds one option and
 * choosing it leaves Write my own; a multiple choice adds to its set.
 * @param {boolean} [on]  multiple choice only: ticked or unticked
 */
export function selectOption(question, state, index, on = true) {
  if (question.kind !== "multiple") return { ...state, chosen: [index], own: false };
  const rest = state.chosen.filter((n) => n !== index);
  return { ...state, chosen: on ? [...rest, index].sort((a, b) => a - b) : rest };
}

/** Selects or unselects Write my own: in a single choice it replaces the options. */
export function selectOwn(question, state, on) {
  if (question.kind === "multiple") return { ...state, own: on };
  return on ? { ...state, chosen: [], own: true } : { ...state, own: false };
}

export const withText = (state, text) => ({ ...state, text });
export const withNote = (state, note) => ({ ...state, note });

/** The recommendation as an answer, or null when the agent gave none. */
function recommendation(question, state) {
  const r = question.recommended;
  if (!r) return null;
  if ("option" in r) return question.kind === "text" ? null : { ...state, chosen: [r.option], own: false };
  return question.kind === "text" ? { ...state, text: r.text } : null;
}

/** Accepts the recommendation of one question; the state when there is none. */
export const acceptOne = (question, state) => recommendation(question, state) ?? state;

/** Fills the blank questions that have a recommendation; an answered one is the same object. */
export function acceptRecommended(questions, states) {
  return questions.map((q, at) => (isAnswered(q, states[at]) ? states[at] : acceptOne(q, states[at])));
}

/** How many questions have an answer, and how many a send would skip. */
export function progress(questions, states) {
  const answered = questions.filter((q, at) => isAnswered(q, states[at])).length;
  return { answered, total: questions.length, skipped: questions.length - answered };
}

/** The index of the first question a send would skip, or -1 when none: where Review skipped goes. */
export const firstBlank = (questions, states) => questions.findIndex((q, at) => !isAnswered(q, states[at]));

/**
 * The contract's answers: a skipped question carries nothing, an answered one
 * its options, its own text (only while Write my own is selected) and its note.
 * @returns {import("./interviews.js").Answer[]}
 */
export function toAnswers(questions, states) {
  return questions.map((q, at) => {
    const s = states[at];
    if (!isAnswered(q, s)) return { chosen: [], text: "", note: "", skipped: true };
    const text = q.kind === "text" || s.own ? s.text.trim() : "";
    return { chosen: q.kind === "text" ? [] : [...s.chosen], text, note: s.note.trim(), skipped: false };
  });
}

/** Whether a stored state has the shape of one: drafts come from a browser that may hold anything. */
export function isAnswerState(s) {
  return !!s && Array.isArray(s.chosen) && s.chosen.every(Number.isInteger) && typeof s.own === "boolean" && typeof s.text === "string" && typeof s.note === "string";
}
