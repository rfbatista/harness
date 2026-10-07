// Sessions as the markup binds them. The browser twin of the BFF's
// sessions/view.go; web/testdata/views/session-status.json pins both.

import { count, relativeTime } from "../../../shared/presentation/format.js";
import { lastReportOf } from "../domain/channel.js";
import { canResume, group, hasLiveTerminal, isTerminal, needsYou } from "../domain/session.js";

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

/** How a session's agent reads with its mode: "Reviewer", "plain claude as architect". Mirrors RunsAs (view.go). */
export function runsAs(session, agentNames = {}) {
  const agent = agentLabel(session.agentId, agentNames);
  return session.mode ? `${agent} as ${session.mode}` : agent;
}

/**
 * How the session that started this one reads: its agent, or "another
 * session" when it is not among others; "" when a person started it. The BFF's
 * StartedBy (view.go) says the same.
 */
export function startedBy(session, others, agentNames = {}) {
  if (!session.parentSessionId) return "";
  const parent = others.find((s) => s.id === session.parentSessionId);
  return parent ? agentLabel(parent.agentId, agentNames) : "another session";
}

function meta(agent, by, when) {
  return by ? `${agent} · started by ${by} · ${when}` : `${agent} · ${when}`;
}

const GROUP_LABEL = { architect: "Architect", "needs-you": "Needs you", active: "Running", finished: "Earlier" };

/** @returns {{ state: string, word: string }} */
export function statusView(session) {
  if (!isTerminal(session) && session.pendingApprovals > 0) return STATUS.waiting_approval;
  return STATUS[session.status];
}

/** How far a delegate's row is indented: deeper ones line up at the last step. */
export const MAX_DEPTH = 3;

/**
 * messages: the task's messages once loaded (undefined before), for a
 * delegate's last report.
 */
export function toRowView(session, { selectedId, now, agentNames, others = [], fresh = false, depth = 0, messages }) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    meta:
      session.role === "delegate"
        ? delegateMeta(session, { now, agentNames, messages })
        : meta(runsAs(session, agentNames), startedBy(session, others, agentNames), relativeTime(session.updatedAt, now)),
    selected: session.id === selectedId,
    fresh,
    attention: needsYou(session),
    resumable: terminalView(session).resumable,
    resumeLabel: `Resume ${session.task || "Untitled session"}`,
    role: session.role,
    depth: Math.min(depth, MAX_DEPTH),
    badge: session.role === "architect" ? "architect" : "",
  };
}

/** fresh: the ids of sessions that just arrived over the feed, highlighted for a moment. */
export function toGroupViews(sessions, { selectedId, now, agentNames, fresh = new Set(), messages }) {
  return group(sessions).map(({ key, sessions: members, depth = {} }) => ({
    key,
    label: GROUP_LABEL[key],
    tone: key === "needs-you" ? "attention" : null,
    count: members.length,
    rows: members.map((s) => toRowView(s, { selectedId, now, agentNames, others: sessions, fresh: fresh.has(s.id), depth: depth[s.id] ?? 0, messages })),
  }));
}

/**
 * A delegate's faint line: its agent, its last report (once the messages are
 * loaded) and its status check. Who started it shows in the tree, so it is
 * not repeated; when it changed shows only when nothing else does. The BFF's
 * delegateMeta (view.go) renders the same before the messages load.
 */
function delegateMeta(session, { now, agentNames, messages }) {
  const parts = [runsAs(session, agentNames)];
  if (messages) parts.push(reportView(lastReportOf(messages, session.id), now));
  if (session.statusCheck) parts.push(statusCheckView(session.statusCheck, now).short);
  if (parts.length === 1) parts.push(relativeTime(session.updatedAt, now));
  return parts.join(" · ");
}

const REPORT_WORD = { working: "working", blocked: "blocked", ready_for_review: "ready for review", done: "done" };

/** A delegate's last report as its row says it: "ready for review 4m"; "no report yet". */
export function reportView(report, now) {
  if (!report) return "no report yet";
  return `${REPORT_WORD[report.status] ?? "reported"} ${relativeTime(report.createdAt, now)}`;
}

/** How long until then, as the meta column writes it: "6m", "1h"; "" when it is due. */
function until(then, now) {
  if (!then || then.getTime() - now.getTime() < 60_000) return "";
  return relativeTime(now, then);
}

const CHECK_STATE = { active: "running", paused: "idle", ended: "done" };

/**
 * A status-check loop as the page shows it: the .status state, the row's
 * short word, and the header's detail. The BFF's StatusCheckView (view.go)
 * says the same; web/testdata/views/status-check.json pins both.
 * @param {import("../domain/channel.js").StatusCheck} check
 * @returns {{ state: string, short: string, detail: string }}
 */
export function statusCheckView(check, now) {
  const last = check.lastFiredAt
    ? `last ${relativeTime(check.lastFiredAt, now) === "now" ? "just now" : `${relativeTime(check.lastFiredAt, now)} ago`} · ${count(check.firedCount, "check")}`
    : "no check yet";
  if (check.state !== "active") {
    const word = check.state === "paused" ? "checks paused" : "checks ended";
    return { state: CHECK_STATE[check.state], short: word, detail: `${word} · ${last}` };
  }
  const next = until(check.nextAt, now);
  const every = relativeTime(new Date(0), new Date(check.everyMinutes * 60_000));
  return {
    state: CHECK_STATE.active,
    short: next ? `check in ${next}` : "check due",
    detail: `checks every ${every} · ${next ? `next in ${next}` : "next due now"} · ${last}`,
  };
}

export function toDetailView(session, now, agentNames) {
  const status = statusView(session);
  return {
    id: session.id,
    title: session.task || "Untitled session",
    state: status.state,
    word: status.word,
    agent: runsAs(session, agentNames),
    lastAction: session.lastAction || "Nothing yet.",
    updated: relativeTime(session.updatedAt, now),
    stoppable: !isTerminal(session),
    repositoryId: session.repositoryId,
    ...terminalView(session),
  };
}

/** Why an ended session cannot be resumed, by the server's resume_blocked code. */
const ENDED_NOTE = {
  WORKSPACE_MISSING: "This session has ended and its worktree was removed, so it cannot be resumed. Start a new one.",
  SESSION_TRANSCRIPT_MISSING: "This session has ended and claude kept no conversation for it, so it cannot be resumed. Start a new one.",
};

/**
 * What the detail pane shows where a terminal would be: the live terminal
 * ("attach"), or a note on why there is none. An ended interactive session
 * offers Resume when the server says it would be accepted.
 * @returns {{ terminal: "attach" | "ended" | "elsewhere" | "headless", terminalNote: string, resumable: boolean }}
 */
export function terminalView(session) {
  if (hasLiveTerminal(session)) return { terminal: "attach", terminalNote: "", resumable: false };
  if (!session.interactive) {
    return {
      terminal: "headless",
      terminalNote: `This session runs without a terminal (it was started over the API). Last action: ${session.lastAction || "none yet"}.`,
      resumable: false,
    };
  }
  if (!isTerminal(session)) {
    return {
      terminal: "elsewhere",
      terminalNote: `This session's terminal lives in the TUI on ${session.runnerHost || "another machine"}; attach to it from there.`,
      resumable: false,
    };
  }
  if (canResume(session)) {
    return {
      terminal: "ended",
      terminalNote: "This session has ended; its terminal is gone. Resume it to continue where it stopped, on the same branch and worktree.",
      resumable: true,
    };
  }
  return {
    terminal: "ended",
    terminalNote: ENDED_NOTE[session.resumeBlocked] ?? "This session has ended; its terminal is gone. Start a new one to continue.",
    resumable: false,
  };
}

export function summary(sessions) {
  const waiting = sessions.filter(needsYou).length;
  return waiting > 0 ? `${count(sessions.length, "session")} · ${waiting} waiting` : count(sessions.length, "session");
}
