// Sessions as the markup binds them. The browser twin of the BFF's
// sessions/view.go; web/testdata/views/session-status.json pins both.

import { count, relativeTime } from "../../../shared/presentation/format.js";
import { group, hasLiveTerminal, isTerminal, needsYou } from "../domain/session.js";

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

/** How a session's agent reads: its name, its ID when unknown, "plain claude" without one. */
export function agentLabel(agentId, agentNames = {}) {
  if (!agentId) return "plain claude";
  return agentNames[agentId] || agentId;
}

const GROUP_LABEL = { "needs-you": "Needs you", active: "Running", finished: "Earlier" };

/** @returns {{ state: string, word: string }} */
export function statusView(session) {
  if (!isTerminal(session) && session.pendingApprovals > 0) return STATUS.waiting_approval;
  return STATUS[session.status];
}

export function toRowView(session, { selectedId, now, agentNames }) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    meta: `${agentLabel(session.agentId, agentNames)} · ${relativeTime(session.updatedAt, now)}`,
    selected: session.id === selectedId,
    attention: needsYou(session),
  };
}

export function toGroupViews(sessions, { selectedId, now, agentNames }) {
  return group(sessions).map(({ key, sessions: members }) => ({
    key,
    label: GROUP_LABEL[key],
    tone: key === "needs-you" ? "attention" : null,
    count: members.length,
    rows: members.map((s) => toRowView(s, { selectedId, now, agentNames })),
  }));
}

export function toDetailView(session, now, agentNames) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    agent: agentLabel(session.agentId, agentNames),
    lastAction: session.lastAction || "Nothing yet.",
    updated: relativeTime(session.updatedAt, now),
    stoppable: !isTerminal(session),
    ...terminalView(session),
  };
}

/**
 * What the detail pane shows where a terminal would be: the live terminal
 * ("attach"), or a note on why there is none.
 * @returns {{ terminal: "attach" | "ended" | "elsewhere" | "headless", terminalNote: string }}
 */
export function terminalView(session) {
  if (hasLiveTerminal(session)) return { terminal: "attach", terminalNote: "" };
  if (!session.interactive) {
    return {
      terminal: "headless",
      terminalNote: `This session runs without a terminal (it was started over the API). Last action: ${session.lastAction || "none yet"}.`,
    };
  }
  if (session.runsOn === "tui") {
    return {
      terminal: "elsewhere",
      terminalNote: `This session's terminal lives in the TUI on ${session.runnerHost || "another machine"}; attach to it from there.`,
    };
  }
  return { terminal: "ended", terminalNote: "This session has ended; its terminal is gone. Resume it from the TUI, or start a new one." };
}

export function summary(sessions) {
  const waiting = sessions.filter(needsYou).length;
  return waiting > 0 ? `${count(sessions.length, "session")} · ${waiting} waiting` : count(sessions.length, "session");
}
