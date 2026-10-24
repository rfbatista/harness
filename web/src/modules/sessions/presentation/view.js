// Sessions as the markup binds them. The browser twin of the BFF's
// sessions/view.go; web/testdata/views/session-status.json pins both.

import { factsView } from "./sandboxView.js";
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

/** runsAs with the model, as a session's row says it: "plain claude as architect on opus". Mirrors RunsOn (view.go). */
export function runsOn(session, agentNames = {}) {
  const agent = runsAs(session, agentNames);
  return session.model ? `${agent} on ${session.model}` : agent;
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

/** True only for a session that started with the sandbox off; one from before the sandbox is not. */
export function isUnsandboxed(session) {
  return session.sandbox != null && !session.sandbox.sandboxed;
}

const GROUP_LABEL = { architect: "Architect", "needs-you": "Needs you", active: "Running", finished: "Earlier" };

/** @returns {{ state: string, word: string }} */
export function statusView(session) {
  if (!isTerminal(session) && session.pendingApprovals > 0) return STATUS.waiting_approval;
  return STATUS[session.status];
}

/** .status[data-state] → an option's data-tone (the dot in a Listbox select). */
const TONE = { running: "signal", waiting: "attention", failed: "danger" };

/**
 * A delegate as an option of the Conversation filter: who and on what, with
 * its status word and dot.
 * @returns {{ id: string, label: string, word: string, tone: string }}
 */
export function delegateOption(session, agentNames = {}) {
  const status = statusView(session);
  return {
    id: session.id,
    label: `${agentLabel(session.agentId, agentNames)} · ${session.task || "Untitled session"}`,
    word: status?.word ?? "",
    tone: TONE[status?.state] ?? "",
  };
}

/** How far a delegate's row is indented: deeper ones line up at the last step. */
export const MAX_DEPTH = 3;

/**
 * messages: the task's messages once loaded (undefined before), for a
 * delegate's last report.
 */
/**
 * A session's row title: what it was asked to do; with no prompt, "Untitled
 * session", or "Feature brainstorm" for a session with no task (where a task's
 * title would otherwise say what it is about). The BFF's RowTitle (view.go)
 * says the same; web/testdata/views/session-title.json pins both.
 */
export function rowTitle(session) {
  if (session.task) return session.task;
  return session.ticketId ? "Untitled session" : "Feature brainstorm";
}

/** The project's sessions page (its feature brainstorms), with one session open. */
export function projectSessionsHref(projectId, sessionId = "") {
  const href = `/projects/${encodeURIComponent(projectId)}/sessions`;
  return sessionId ? `${href}?session=${encodeURIComponent(sessionId)}` : href;
}

/** The status word's chip tone on a row: only the uncommon states have a chip. */
const CHIP_TONE = { thinking: "signal", "your turn": "attention", approval: "attention", failed: "danger" };
const NO_CHIP = new Set(["running", "done"]);

/** A row's status chip, null for running and done. */
function chipOf(status) {
  return NO_CHIP.has(status.word) ? null : { word: status.word, tone: CHIP_TONE[status.word] ?? null };
}

const quoted = (text) => `“${text.replace(/\s+/g, " ").trim()}”`;

/** A report's status as the row's detail kind. */
const REPORT_KIND = { working: "working", ready_for_review: "review", blocked: "blocked", done: "done" };

/**
 * The detail under a row's first line: approvals pending, a delegate's report
 * and check, who started it, or a prompt that is not the task's title; null
 * when there is none. The BFF's rowDetail (view.go) says the same before the
 * messages load; web/testdata/views/session-row.json pins both.
 * @returns {{ kind: string, lead: string, text: string, check: string } | null}
 */
function detailOf(session, { now, agentNames, others, taskTitle, report, loaded }) {
  if (!isTerminal(session) && session.pendingApprovals > 0) {
    const n = session.pendingApprovals;
    return { kind: "approval", lead: n === 1 ? "1 approval pending" : `${n} approvals pending`, text: "", check: "" };
  }
  if (session.role === "delegate") {
    const check = session.statusCheck ? statusCheckView(session.statusCheck, now).short : "";
    if (!loaded) return check ? { kind: "none", lead: "", text: "", check } : null;
    if (!report) return { kind: "none", lead: "no report yet", text: "", check };
    return {
      kind: REPORT_KIND[report.status] ?? "working",
      lead: REPORT_WORD[report.status] ?? "reported",
      text: report.body ? quoted(report.body) : "",
      check,
    };
  }
  const by = startedBy(session, others, agentNames);
  if (by) return { kind: "by", lead: "started by", text: by, check: "" };
  if (session.role !== "architect" && taskTitle && session.task && session.task !== taskTitle) {
    return { kind: "prompt", lead: "prompt", text: quoted(session.task), check: "" };
  }
  return null;
}

/**
 * A row as the markup binds it: its agent (a brainstorm's title), mode and
 * model, a status chip for the uncommon states, when, and the detail. The
 * BFF's toRow (view.go) says the same; session-row.json pins both. taskTitle
 * is the page's task, whose title no row repeats.
 */
export function toRowView(session, { selectedId, now, agentNames, others = [], fresh = false, depth = 0, messages, taskTitle = "" }) {
  const status = statusView(session);
  const leadsWithTitle = !session.ticketId;
  const agent = leadsWithTitle ? rowTitle(session) : agentLabel(session.agentId, agentNames);
  const architect = session.role === "architect";
  const loaded = session.role === "delegate" && messages !== undefined;
  const report = loaded ? lastReportOf(messages, session.id) : null;
  const detail = detailOf(session, { now, agentNames, others, taskTitle, report, loaded });
  const when = relativeTime(report ? report.createdAt : session.updatedAt, now);
  const mode = architect ? "" : session.mode || "";
  const model = session.model || "";
  const facts = [detail ? [detail.lead, detail.text].filter(Boolean).join(" ") : "", detail?.check];
  return {
    id: session.id,
    agent,
    leadsWithTitle,
    // What x-bind sets: null leaves the attribute off a row led by its agent.
    leadAttr: leadsWithTitle ? "title" : null,
    mode,
    model,
    badge: architect ? "architect" : "",
    chip: chipOf(status),
    state: status.state,
    word: status.word,
    when,
    detail,
    // Every fact the detail's clamp may hide, for the row's tooltip.
    detailText: [agent, mode, model, isUnsandboxed(session) ? "unsandboxed" : "", ...facts, status.word, when].filter(Boolean).join(" · "),
    // What x-bind sets: null leaves the attribute off a sandboxed row.
    unsandboxed: isUnsandboxed(session),
    unsandboxedAttr: isUnsandboxed(session) ? "" : null,
    selected: session.id === selectedId,
    fresh,
    attention: needsYou(session),
    resumable: terminalView(session).resumable,
    resumeLabel: `Resume ${agent}`,
    role: session.role,
    depth: Math.min(depth, MAX_DEPTH),
    // What x-bind sets: null leaves the attribute off a peer's row.
    roleAttr: session.role || null,
    depthAttr: depth > 0 ? String(Math.min(depth, MAX_DEPTH)) : null,
    workspaceId: session.workspaceId,
  };
}

/** fresh: the ids of sessions that just arrived over the feed, highlighted for a moment. */
export function toGroupViews(sessions, { selectedId, now, agentNames, fresh = new Set(), messages, taskTitle = "" }) {
  return group(sessions).map(({ key, sessions: members, depth = {} }) => ({
    key,
    label: GROUP_LABEL[key],
    tone: key === "needs-you" ? "attention" : null,
    count: members.length,
    rows: members.map((s) => toRowView(s, { selectedId, now, agentNames, others: sessions, fresh: fresh.has(s.id), depth: depth[s.id] ?? 0, messages, taskTitle })),
  }));
}

const REPORT_WORD = { working: "working", blocked: "blocked", ready_for_review: "ready for review", done: "done" };

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
    return { state: CHECK_STATE[check.state], short: word, detail: `${word} · ${last}`, next: "", last };
  }
  const next = until(check.nextAt, now);
  const every = relativeTime(new Date(0), new Date(check.everyMinutes * 60_000));
  return {
    state: CHECK_STATE.active,
    short: next ? `check in ${next}` : "check due",
    detail: `checks every ${every} · ${next ? `next in ${next}` : "next due now"} · ${last}`,
    next: next ? `next in ${next}` : "next due now",
    last,
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
    /** Who the agent's messages in History are from: its name, "claude" without one. */
    speaker: session.agentId ? agentLabel(session.agentId, agentNames) : "claude",
    lastAction: session.lastAction || "Nothing yet.",
    updated: relativeTime(session.updatedAt, now),
    stoppable: !isTerminal(session),
    repositoryId: session.repositoryId,
    workspaceId: session.workspaceId,
    branch: session.branch,
    ...tokenSaversView(session),
    ...sandboxView(session),
    sandboxFacts: factsView(session),
    ...terminalView(session),
  };
}

const VISIBILITY_WORDS = { "own repo": "Own worktree only", "all repos, read-only": "All repos read-only", unrestricted: "No repository limits" };

/**
 * The chips of a session's sandbox facts (UX 6): quiet "Sandboxed" and its
 * repository visibility, or the amber "Not sandboxed" when it started with
 * the sandbox off. A session from before the sandbox has none.
 * @returns {{ sandboxChip: string, sandboxTone: string | null, visibilityChip: string }}
 */
export function sandboxView(session) {
  const f = session.sandbox;
  if (!f) return { sandboxChip: "", sandboxTone: null, visibilityChip: "" };
  // Every mode but product writes its own worktree; with all repos readable it reads the rest.
  const architect = session.mode !== "product" && f.visibility === "all repos, read-only";
  return {
    sandboxChip: f.sandboxed ? "Sandboxed" : "Not sandboxed",
    sandboxTone: f.sandboxed ? null : "attention",
    visibilityChip: architect ? "Own worktree + all repos read-only" : (VISIBILITY_WORDS[f.visibility] ?? f.visibility),
  };
}

/**
 * The token savers a session runs with, beside its usage, and the ones on
 * for it that were not installed.
 * @returns {{ tokenSavers: string, tokenSaversMissing: string, tokenSaversMissingTitle: string }}
 */
export function tokenSaversView(session) {
  const missing = session.tokenSaversNotInstalled ?? [];
  const names = missing.join(", ");
  return {
    tokenSavers: (session.tokenSavers ?? []).join(" · "),
    tokenSaversMissing: missing.length ? `${names} not installed` : "",
    tokenSaversMissingTitle: missing.length
      ? `${names} ${missing.length === 1 ? "was" : "were"} on for this session but not installed, so it runs without ${missing.length === 1 ? "it" : "them"}. Install ${missing.length === 1 ? "it" : "them"} on the Token savers page.`
      : "",
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
