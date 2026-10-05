// Sessions as the markup binds them. The browser twin of the BFF's
// sessions/view.go; web/testdata/views/session-status.json pins both.

import { count, relativeTime } from "../../../shared/presentation/format.js";
import { acceptsInput, group, isTerminal, needsYou } from "../domain/session.js";

/** Domain status → the design system's .status[data-state] and its word. */
const STATUS = {
  starting: { state: "running", word: "starting" },
  running: { state: "running", word: "running" },
  thinking: { state: "running", word: "thinking" },
  idle: { state: "waiting", word: "your turn" },
  waiting_approval: { state: "waiting", word: "approval" },
  paused: { state: "idle", word: "paused" },
  done: { state: "done", word: "done" },
  stopped: { state: "done", word: "stopped" },
  failed: { state: "failed", word: "failed" },
};

const GROUP_LABEL = { "needs-you": "Needs you", active: "Running", finished: "Earlier" };

/** @returns {{ state: string, word: string }} */
export function statusView(session) {
  if (!isTerminal(session) && session.pendingApprovals > 0) return STATUS.waiting_approval;
  return STATUS[session.status];
}

export function toRowView(session, { selectedId, now }) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    meta: `${session.agentId || "no agent"} · ${relativeTime(session.updatedAt, now)}`,
    selected: session.id === selectedId,
    attention: needsYou(session),
  };
}

export function toGroupViews(sessions, { selectedId, now }) {
  return group(sessions).map(({ key, sessions: members }) => ({
    key,
    label: GROUP_LABEL[key],
    tone: key === "needs-you" ? "attention" : null,
    count: members.length,
    rows: members.map((s) => toRowView(s, { selectedId, now })),
  }));
}

export function toDetailView(session, now) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    agent: session.agentId || "no agent",
    lastAction: session.lastAction || "Nothing yet.",
    updated: relativeTime(session.updatedAt, now),
    acceptsInput: acceptsInput(session),
    stoppable: !isTerminal(session),
  };
}

export function summary(sessions) {
  const waiting = sessions.filter(needsYou).length;
  return waiting > 0 ? `${count(sessions.length, "session")} · ${waiting} waiting` : count(sessions.length, "session");
}
