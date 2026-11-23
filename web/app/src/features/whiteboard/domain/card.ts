// A card: one session drawn as the session row draws it (ADR 0029, ADR 0108).

import { isTerminal, type BoardSession } from "./session";

export interface StatusView {
  state: "running" | "waiting" | "idle" | "done" | "failed";
  word: string;
}

const STATUS: Record<BoardSession["status"], StatusView> = {
  starting: { state: "running", word: "starting" },
  running: { state: "running", word: "running" },
  thinking: { state: "running", word: "thinking" },
  idle: { state: "waiting", word: "waiting for you" },
  waiting_approval: { state: "waiting", word: "approval" },
  paused: { state: "idle", word: "paused" },
  done: { state: "done", word: "done" },
  stopped: { state: "done", word: "stopped" },
  failed: { state: "failed", word: "failed" },
};

export const INTERRUPTED_WORD = "interrupted by restart";

/** The dot and the word of a session, as the row says them. */
export function statusView(s: BoardSession): StatusView {
  if (!isTerminal(s) && s.pendingApprovals > 0) return STATUS.waiting_approval;
  const status = STATUS[s.status];
  if (s.status === "stopped" && s.interruptedAt) return { ...status, word: INTERRUPTED_WORD };
  if (!isTerminal(s) && s.status !== "waiting_approval" && s.openInterviewCount > 0) return { ...status, word: "questions" };
  return status;
}

/** A chip for the uncommon states: the dot says the common ones. */
const NO_CHIP = new Set(["running", "starting", "thinking", "done", "waiting for you"]);
export function chipOf(status: StatusView): string {
  return NO_CHIP.has(status.word) ? "" : status.word;
}

export function agentLabel(agentId: string, agentNames: Readonly<Record<string, string>>): string {
  if (!agentId) return "plain claude";
  return agentNames[agentId] || agentId;
}

const quoted = (text: string): string => `“${text.replace(/\s+/g, " ").trim()}”`;

const REPORT_WORD: Record<string, string> = { working: "working", blocked: "blocked", ready_for_review: "ready for review", done: "done" };

export interface Detail {
  lead: string;
  text: string;
}

export interface DetailContext {
  taskTitle: string;
  agentNames: Readonly<Record<string, string>>;
  others: readonly BoardSession[];
  /** The delegate's latest report on the task channel, once the messages are loaded. */
  report: { status: string; body: string } | null;
  messagesLoaded: boolean;
}

/** The row's detail line: approvals, a delegate's report, who started it, or a prompt that is not the task's title. */
export function detailOf(s: BoardSession, ctx: DetailContext): Detail | null {
  if (!isTerminal(s) && s.pendingApprovals > 0) {
    return { lead: s.pendingApprovals === 1 ? "1 approval pending" : `${s.pendingApprovals} approvals pending`, text: "" };
  }
  const parent = ctx.others.find((o) => o.id === s.parentSessionId);
  const by = s.parentSessionId ? (parent ? agentLabel(parent.agentId, ctx.agentNames) : "another session") : "";
  if (s.role === "delegate") {
    const brought = { lead: "brought under", text: by };
    if (!ctx.messagesLoaded) return s.broughtUnder ? brought : null;
    if (!ctx.report) return s.broughtUnder ? brought : { lead: "no report yet", text: "" };
    return { lead: REPORT_WORD[ctx.report.status] ?? "reported", text: ctx.report.body ? quoted(ctx.report.body) : "" };
  }
  if (by) return { lead: s.broughtUnder ? "brought under" : "started by", text: by };
  if (s.role !== "architect" && ctx.taskTitle && s.task && s.task !== ctx.taskTitle) return { lead: "prompt", text: quoted(s.task) };
  return null;
}

/**
 * Waiting on the person: an approval, an open interview, or an interactive
 * session at its turn boundary.
 */
export const needsYou = (s: BoardSession): boolean =>
  !isTerminal(s) && (s.status === "waiting_approval" || s.status === "idle" || s.pendingApprovals > 0 || s.openInterviewCount > 0);
