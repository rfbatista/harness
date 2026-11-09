// The interview panel as the markup binds it. The wording is the UX study's
// section 10 ("UX study: Interview panel"): change it there first.

import { renderMarkdown } from "../../../shared/domain/markdown.js";
import { count } from "../../../shared/presentation/format.js";
import { historyOf, openOnes, openQuestionCount } from "../domain/interviews.js";
import { canSend, WAITING_FOR_CONNECTION } from "../domain/sending.js";

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
    at,
    kind: question.kind,
    number: String(at + 1),
    title: question.title,
    body: question.body,
    name: `${interviewId}.${at}`,
    multiple: question.kind === "multiple",
    text: question.kind === "text",
    hint: question.kind === "multiple" ? "Choose any number" : "",
    options,
    own: choice ? { id: `${interviewId}.${at}.own`, label: "Write my own" } : null,
    ownLabel: `Your own answer to: ${question.title}`,
    noteLabel: `Note on: ${question.title}`,
    recommendedText: question.recommended && "text" in question.recommended ? question.recommended.text : "",
  };
}

/** How many questions the agent recommends an answer to: the bulk line's "3 recommended". */
export const recommendedCount = (questions) => questions.filter((q) => q.recommended).length;

/** The bulk line's tooltip: what the button does and does not do. */
export const ACCEPT_ALL_TITLE = "Fill every blank question with the agent's recommendation. Nothing is sent.";

/**
 * The footer's words for how far the interview is: "3 of 5 answered · 2 will
 * be skipped", the Send button that says what happens, and the bar's fill.
 * @param {{ answered: number, total: number, skipped: number }} p
 */
export function footerView({ answered, total, skipped }) {
  const send = answered === 0 ? "Send, all skipped" : skipped > 0 ? `Send · ${skipped} skipped` : "Send answers";
  return {
    count: `${answered} of ${total}`,
    rest: skipped > 0 ? ` answered · ${skipped} will be skipped` : " answered",
    text: `${answered} of ${total} answered${skipped > 0 ? ` · ${skipped} will be skipped` : ""}`,
    send,
    bar: `inline-size: ${total === 0 ? 0 : Math.round((answered / total) * 100)}%`,
  };
}

/** The banner on an interview the person just sent from this page. */
export const SENT_BANNER = "Answers sent. The agent reads them as its next message, after its current turn.";

/** The banner on an interview whose send found the session ended. */
export const EXPIRED_BANNER = "The session ended before the agent could read your answers. Copy them if you need them.";

const FAILED = {
  send: { title: "Could not send your answers.", sending: "Sending your answers…", button: "Sending…" },
  dismiss: { title: "Could not dismiss the interview.", sending: "Dismissing the interview…", button: "Dismissing…" },
};

/**
 * The footer of an open interview: how far it is, the Send button, and what a
 * step of sending or dismissing puts in its place (the strip that asks, the
 * status while it goes, the banner of a failure). Wording is the UX study's.
 * @param {{ answered: number, total: number, skipped: number }} progress
 * @param {import("../domain/sending.js").Step} step
 * @param {{ connection: string, note: string }} state  the page stream's status, and the note typed into the dismiss confirm
 */
export function footView(progress, step, { connection, note }) {
  const base = footerView(progress);
  const live = canSend(connection);
  const sending = step.phase === "sending";
  const failure = step.phase === "failed" ? step.error : null;
  const wording = FAILED[step.kind ?? "send"];
  const isSend = step.kind === "send";
  return {
    ...base,
    phase: step.phase,
    kind: step.kind,
    // What the footer draws: the progress line while the person decides, one strip that asks, the banner of a failure, the buttons unless a strip asks.
    sumShown: step.phase === "idle" || (step.phase === "confirming" && isSend),
    stripSend: step.phase === "confirming" && isSend,
    stripDismiss: step.phase === "confirming" && step.kind === "dismiss",
    errorShown: failure !== null,
    actionsShown: step.phase !== "confirming",
    locked: sending,
    busy: sending,
    dismissDisabled: sending,
    sendDisabled: sending || !live,
    reason: !live && !sending ? WAITING_FOR_CONNECTION : "",
    status: sending ? wording.sending : "",
    send: sending && isSend ? wording.button : failure?.retry ? "Try again" : base.send,
    confirm: {
      title: `Send with ${progress.skipped} skipped?`,
      text: "The agent reads “skipped” for those questions. You cannot edit after sending.",
      review: "Review skipped",
      send: `Send ${progress.total} answers`,
    },
    dismiss: {
      title: "Dismiss this interview?",
      text: "The agent is told you chose not to answer. Your typed answers are discarded. You cannot undo it.",
      keep: "Keep answering",
      confirm: sending && !isSend ? wording.button : "Dismiss interview",
      note,
    },
    error: failure && {
      code: failure.code,
      title: wording.title,
      text: failure.retry ? "The harness did not answer. Your answers are kept here." : failure.message,
      tail: failure.retry ? "INTERVIEW_SEND_FAILED · try again" : failure.code,
      retry: failure.retry,
    },
  };
}

/** The option the number key `key` selects in the drawn order ("1" is the first row), or null: no such row, not a digit, or past the 8 options a question holds. */
export function optionForKey(question, key) {
  if (!/^[1-8]$/.test(key)) return null;
  return question.options[Number(key) - 1] ?? null;
}

/**
 * An open interview as its card draws it. A folded card is a row: who asks,
 * its first question as a second line, and how far the person got.
 * @param {{ folded?: boolean, progress?: { answered: number, total: number } }} [state]
 */
export function cardView(interview, { folded = false, progress } = {}) {
  const n = interview.questions.length;
  const { answered, total } = progress ?? { answered: 0, total: n };
  const label = `Open interview from ${interview.agentName}`;
  return {
    id: interview.id,
    label,
    foldLabel: `${label}, folded`,
    foldButton: `Fold the interview from ${interview.agentName}`,
    folded,
    foldedSub: interview.questions[0]?.title ?? "",
    foldedBadge: `${answered} of ${total}`,
    heading: `${interview.agentName} asks ${count(n, "question")}`,
    asked: `Asked ${clockTime(interview.askedAt)}`,
    recommended: recommendedCount(interview.questions),
    recommendedLabel: `${recommendedCount(interview.questions)} recommended`,
    questions: interview.questions.map((q, at) => questionView(q, at, interview.id)),
  };
}

/** The note disclosure's summary: it says Note once it holds text, and Add a note while it is empty. */
export const noteSummary = (text) => (text.trim() === "" ? "Add a note" : "Note");

/** What the polite region says when an interview that was open here is closed by something else (another tab, the server). */
export function closedText(interview) {
  if (interview.state === "sent") return "Interview answered in another tab";
  if (interview.state === "dismissed") return "Interview dismissed in another tab";
  return "Interview expired: the session ended";
}

/** An answer read back: the options chosen, then the text; null when it holds nothing (skipped). */
function answerText(question, answer) {
  if (!answer || answer.skipped) return null;
  const parts = answer.chosen.map((i) => question.options[i]?.label).filter(Boolean);
  if (answer.text.trim() !== "") parts.push(answer.text.trim());
  return parts.length > 0 ? parts.join(", ") : null;
}

/** The questions of a closed interview with what was answered to each, "Skipped" for a blank. */
function readBack(interview, answers) {
  return interview.questions.map((q, at) => {
    const text = answerText(q, answers?.[at]);
    return {
      key: `${interview.id}.${at}`,
      number: String(at + 1),
      title: q.title,
      heading: `${at + 1} · ${q.title}`,
      answer: text ?? "Skipped",
      answered: text !== null,
      skipped: text === null,
      note: answers?.[at]?.note?.trim() ?? "",
    };
  });
}

/** The answers as plain text, for Copy answers: each question with its answer, then its note. */
export function answersText(interview, answers) {
  return readBack(interview, answers)
    .map((q) => [`${q.number}. ${q.title}`, q.answer, ...(q.note ? [`Note: ${q.note}`] : [])].join("\n"))
    .join("\n\n");
}

/** What a sent or expired interview says over its answers, where this page sent it: the tone of its banner and how it is announced. */
const BANNER = { sent: { text: SENT_BANNER, tone: "signal", role: "status" }, expired: { text: EXPIRED_BANNER, tone: "danger", role: "alert" } };

const WORD = { sent: "Sent", dismissed: "Dismissed", expired: "Expired" };
const TONE = { sent: "success", dismissed: null, expired: "danger" };

/** The folded row's second line: counts and delivery for a sent one, the end for the others. */
function subOf(interview, answers) {
  const ended = clockTime(interview.endedAt ?? interview.askedAt);
  if (interview.state === "dismissed") return `Dismissed ${ended} · the agent was told`;
  if (interview.state === "expired") return `Session ended ${ended} · the agent never read it`;
  const done = (answers ?? []).filter((a) => !a.skipped).length;
  const skipped = (answers ?? []).filter((a) => a.skipped).length;
  const counts = `${done} answered${skipped > 0 ? `, ${skipped} skipped` : ""}`;
  if (interview.delivery === "queued") return `${counts} · queued: the agent is mid-turn`;
  return `${counts} · ${interview.deliveredAt ? `delivered ${clockTime(interview.deliveredAt)}` : `sent ${ended}`}`;
}

/**
 * A sent, dismissed or expired interview as its row in Earlier draws it, folded
 * or opened. `typed` is what the person had typed, for an expired interview
 * (the server keeps no answers for it); `here`: the person sent it from this
 * page, so a sent or expired one carries its banner.
 * @param {import("../domain/interviews.js").Interview} interview
 * @param {readonly import("../domain/interviews.js").Answer[] | null} typed
 */
export function historyRowView(interview, typed = null, here = false) {
  const answers = interview.answers ?? typed;
  const at = clockTime(interview.askedAt);
  const word = WORD[interview.state];
  return {
    id: interview.id,
    state: interview.state,
    label: `${word} interview from ${interview.agentName} at ${at}`,
    title: `${word} · ${interview.agentName} · ${at}`,
    sub: subOf(interview, answers),
    banner: here ? (BANNER[interview.state]?.text ?? "") : "",
    bannerTone: BANNER[interview.state]?.tone ?? "",
    bannerRole: BANNER[interview.state]?.role ?? "",
    badge: count(interview.questions.length, "question"),
    tone: TONE[interview.state],
    copy: interview.state === "expired",
    dismissNote: interview.dismissNote ?? "",
    questions: readBack(interview, answers),
  };
}

export const EMPTY_TITLE = "No questions right now";
export const EMPTY_TEXT = "When an agent asks you questions, they appear here, and your answers go back to it automatically.";

/**
 * The whole panel for a list: the open interviews newest first (the study's
 * order), the newest expanded and the others folded unless the person said
 * otherwise, then Earlier.
 * @param {object} [state]  what the store knows: the person's fold choices, how far each interview is, the rows opened, what an expired interview had typed
 * @param {(id: string) => boolean | undefined} [state.folded]
 * @param {(id: string) => { answered: number, total: number }} [state.progress]
 * @param {(id: string) => boolean} [state.opened]
 * @param {(interview: import("../domain/interviews.js").Interview) => readonly import("../domain/interviews.js").Answer[] | null} [state.typed]
 * @param {(id: string) => boolean} [state.sentHere]  the interview was sent from this page
 */
export function panelView(list, { folded = () => undefined, progress = () => undefined, opened = () => false, typed = () => null, sentHere = () => false } = {}) {
  const open = openOnes(list);
  const n = openQuestionCount(list);
  const history = historyOf(list);
  return {
    open: open.length > 0,
    questions: n,
    openLabel: `${n} open`,
    toggleLabel: toggleLabel(n),
    who: open.length > 0 ? headWho(open) : history.length > 0 ? "nothing open" : "",
    peek: open.length > 0 ? peekText(n, open[open.length - 1].agentName) : "",
    cards: [...open].reverse().map((i, at) => ({ ...cardView(i, { folded: folded(i.id) ?? at > 0, progress: progress(i.id) }), foldable: open.length > 1 })),
    hasHistory: history.length > 0,
    empty: open.length === 0 && history.length > 0,
    emptyTitle: EMPTY_TITLE,
    emptyText: EMPTY_TEXT,
    history: {
      label: `Earlier · ${history.length}`,
      rows: history.map((i) => ({ ...historyRowView(i, typed(i), sentHere(i.id)), opened: opened(i.id) })),
    },
  };
}

/** A question's body as HTML: markdown the web UI's way, raw HTML shown as text. */
export const bodyHtml = (markdown) => renderMarkdown(markdown ?? "");
