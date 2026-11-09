// What the Agent board shows of its state: sections, cards and session lines
// as plain values the markup binds, the summary line and the filter. The Go
// BFF paints the first paint with the same rules (internal/adapter/in/web/home/board.go).

import { count, relativeTime } from "../../../shared/presentation/format.js";
import { FINISHED, liveCards } from "../domain/board.js";

/** Lines a card shows before "+N more". */
export const COLLAPSE_AT = 3;

const LOOK = {
  starting: { dot: "queued", word: "starting", tone: null, chip: true, rank: 3 },
  running: { dot: "running", word: "running", tone: null, chip: false, rank: 3 },
  thinking: { dot: "running", word: "thinking", tone: "signal", chip: true, rank: 3 },
  idle: { dot: "waiting", word: "your turn", tone: "attention", chip: true, rank: 2 },
  waiting_approval: { dot: "waiting", word: "approval", tone: "attention", chip: true, rank: 0 },
  // A live session with an open interview, whatever its status says (not an approval).
  questions: { dot: "waiting", word: "questions", tone: "attention", chip: true, rank: 1 },
  paused: { dot: "idle", word: "paused", tone: null, chip: true, rank: 3 },
  [FINISHED]: { dot: "done", word: "finished", tone: null, chip: true, rank: 3 },
};

/** The look of a line: its status's, or "questions" when an interview waits and no approval does. */
const lookOf = (line) => {
  const status = line.openInterviewCount > 0 && line.status !== "waiting_approval" && line.status !== FINISHED ? "questions" : line.status;
  return LOOK[status] ?? { dot: "idle", word: status, tone: null, chip: true, rank: 3 };
};

const TASK_STATUS = { backlog: "Backlog", todo: "Todo", in_progress: "In progress", review: "Review", done: "Done" };

const enc = encodeURIComponent;
export const taskHref = (card) => `/projects/${enc(card.projectId)}/tasks/${enc(card.taskId)}`;
export const sessionHref = (card, sessionId) => `${taskHref(card)}?session=${enc(sessionId)}`;

/** Approvals first, then questions, then your turn, then the rest in the order the server sent. */
export const orderedLines = (sessions) =>
  [0, 1, 2, 3].flatMap((rank) => sessions.filter((s) => lookOf(s).rank === rank));

/** The word a session that needs the person is announced with ("approval", "your turn"). */
export const needsWord = (card) => {
  const line = orderedLines(card.sessions)[0];
  return line ? lookOf(line).word : "";
};

const hover = (s, since) =>
  [s.agentName, s.role, s.mode, s.model, lookOf(s).word, s.waitingFor, since].filter(Boolean).join(" · ");

function lineView(card, s, now, ui) {
  const look = lookOf(s);
  const since = relativeTime(s.statusSince, now);
  return {
    id: s.sessionId,
    href: sessionHref(card, s.sessionId),
    agent: s.agentName,
    dot: look.dot,
    word: look.word,
    tone: look.tone,
    chip: look.chip,
    time: since,
    waitingFor: s.status === "waiting_approval" ? s.waitingFor : null,
    hover: hover(s, since),
    finished: s.status === FINISHED ? true : null,
    fresh: ui.freshLines.includes(`${card.taskId}/${s.sessionId}`) || null,
  };
}

function cardView(card, now, ui) {
  const lines = orderedLines(card.sessions);
  const expanded = ui.expanded.includes(card.taskId);
  const shown = expanded ? lines : lines.slice(0, COLLAPSE_AT);
  const hidden = lines.length - COLLAPSE_AT;
  return {
    id: card.taskId,
    titleId: `t-${card.taskId}`,
    metaId: `m-${card.taskId}`,
    href: taskHref(card),
    title: card.taskTitle,
    feature: card.featureTitle,
    status: TASK_STATUS[card.taskStatus] ?? card.taskStatus,
    needsYou: card.needsYou || null,
    liveWord: count(card.sessions.length, "live session"),
    active: `active ${relativeTime(card.lastStatusChangeAt, now)}`,
    lines: shown.map((s) => lineView(card, s, now, ui)),
    hasMore: hidden > 0,
    expanded,
    moreLabel: expanded ? "Show fewer" : `+${hidden} more`,
    fresh: ui.freshCards.includes(card.taskId) || null,
    arriving: ui.arriving.includes(card.taskId) || null,
    leaving: ui.exiting.includes(card.taskId) || null,
  };
}

/** The filter's text of a card: its project, feature, title and agents. */
const haystack = (section, card) =>
  [section.name, card.taskTitle, card.featureTitle ?? "", ...card.sessions.map((s) => s.agentName)].join(" ").toLowerCase();

/** Sections and cards the filter lets through, as the markup binds them. */
export function sectionsView(board, now, ui) {
  const q = ui.query.trim().toLowerCase();
  return board.sections
    .map((section) => {
      const live = liveCards(section);
      const needs = live.filter((c) => c.needsYou).length;
      const lives = live.reduce((n, c) => n + c.sessions.length, 0);
      return {
        id: section.id,
        headingId: `p-${section.id}`,
        name: section.name,
        href: `/projects/${enc(section.id)}`,
        needs: needs ? `${needs} ${needs === 1 ? "needs" : "need"} you` : "",
        count: `${count(live.length, "task")} · ${lives} live`,
        cards: section.cards.filter((c) => !q || haystack(section, c).includes(q)).map((c) => cardView(c, now, ui)),
      };
    })
    .filter((s) => s.cards.length > 0);
}

/** "12 tasks with 21 live sessions in 4 projects" and "4 need you" apart. */
export function summaryView(board) {
  const sections = board.sections.filter((s) => liveCards(s).length > 0);
  const cards = sections.flatMap(liveCards);
  const lives = cards.reduce((n, c) => n + c.sessions.length, 0);
  const needs = cards.filter((c) => c.needsYou).length;
  return {
    lead: cards.length
      ? `${count(cards.length, "task")} with ${count(lives, "live session")} in ${count(sections.length, "project")}`
      : "No live sessions right now.",
    needs: needs ? `${needs} need you` : "",
    hasNeeds: needs > 0,
    needsCount: needs,
    cards: cards.length,
  };
}

/** "5 quiet projects have no live session." */
export const quietLine = (n) => (n > 0 ? `${n} quiet ${n === 1 ? "project has" : "projects have"} no live session.` : "");

/** "14:02:11", the time the board last changed. */
export const clockTime = (at) => [at.getHours(), at.getMinutes(), at.getSeconds()].map((n) => String(n).padStart(2, "0")).join(":");

/** One sentence for a burst: the first two, then "3 more updates". */
export const announcement = (messages) => {
  const out = messages.length > 3 ? [...messages.slice(0, 2), `${messages.length - 2} more updates`] : messages;
  return `${out.join(". ")}.`;
};
