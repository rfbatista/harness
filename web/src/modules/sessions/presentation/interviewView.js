// The interview panel as the markup binds it. The wording is the UX study's
// section 10 ("UX study: Interview panel"): change it there first.

import { renderMarkdown } from "../../../shared/domain/markdown.js";
import { count } from "../../../shared/presentation/format.js";
import { openOnes, openQuestionCount } from "../domain/interviews.js";

/** A time of day, "14:32". */
export const clockTime = (date) => date.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false });

/** The Questions toggle's accessible name. */
export const toggleLabel = (n) => `Questions, ${n} open`;

/** The peek strip's words: "5 questions from architect". */
export const peekText = (n, agent) => `${count(n, "question")} from ${agent}`;

/** What the polite region says when an interview arrives; `more` when others are already open. */
export function arrivalText(interview, more) {
  const n = interview.questions.length;
  return more ? `${interview.agentName} asked ${n} more question${n === 1 ? "" : "s"}` : `${interview.agentName} asks ${count(n, "question")}`;
}

/** The panel head's second word: who asks, and how many interviews are open past the first. */
export function headWho(open) {
  if (open.length === 0) return "";
  const agents = [...new Set(open.map((i) => i.agentName))].join(", ");
  return open.length > 1 ? `${agents} · ${open.length} open` : agents;
}

/**
 * A question as the card draws it: the number and title, its markdown body,
 * and the answer control. The recommended option is drawn first, never
 * checked; `key` is the number that selects it (the order drawn), `index` is
 * where it is in the interview.
 */
export function questionView(question, at, interviewId) {
  const order = question.options.map((o, index) => ({ index, ...o }));
  const rec = question.recommended && "option" in question.recommended ? question.recommended.option : -1;
  const first = order.filter((o) => o.index === rec);
  const options = [...first, ...order.filter((o) => o.index !== rec)].map((o, n) => ({
    key: String(n + 1),
    value: String(o.index),
    id: `${interviewId}.${at}.${o.index}`,
    label: o.label,
    description: o.description,
    recommended: o.index === rec,
    own: false,
  }));
  const choice = question.kind !== "text";
  return {
    key: `${interviewId}.${at}`,
    number: String(at + 1),
    title: question.title,
    body: question.body,
    name: `${interviewId}.${at}`,
    multiple: question.kind === "multiple",
    text: question.kind === "text",
    hint: question.kind === "multiple" ? "Choose any number" : "",
    options,
    own: choice ? { id: `${interviewId}.${at}.own`, label: "Write my own" } : null,
    recommendedText: question.recommended && "text" in question.recommended ? question.recommended.text : "",
  };
}

/** An open interview as its card draws it. */
export function cardView(interview) {
  const n = interview.questions.length;
  return {
    id: interview.id,
    label: `Open interview from ${interview.agentName}`,
    heading: `${interview.agentName} asks ${count(n, "question")}`,
    asked: `Asked ${clockTime(interview.askedAt)}`,
    questions: interview.questions.map((q, at) => questionView(q, at, interview.id)),
  };
}

/** The whole panel for a list: newest open interview first (the study's order), with the head's words. */
export function panelView(list) {
  const open = openOnes(list);
  const n = openQuestionCount(list);
  return {
    open: open.length > 0,
    questions: n,
    openLabel: `${n} open`,
    toggleLabel: toggleLabel(n),
    who: headWho(open),
    peek: open.length > 0 ? peekText(n, open[open.length - 1].agentName) : "",
    cards: [...open].reverse().map(cardView),
  };
}

/** A question's body as HTML: markdown the web UI's way, raw HTML shown as text. */
export const bodyHtml = (markdown) => renderMarkdown(markdown ?? "");
