// The instruction-file proposals of a repository, in words: what an agent
// proposed (ADR 0092) as the Instructions & rules tab lists it. Pure.

import { count } from "../../../shared/presentation/format.js";

const LABELS = { open: "Open", approved: "Approved", rejected: "Rejected", sent_back: "Sent back" };
const TONES = { open: "attention", approved: "success", rejected: "danger", sent_back: "" };

/** @param {string} state */
export const stateLabel = (state) => LABELS[state] ?? state;

/** The badge tone of a state; "" is the neutral badge. */
export const stateTone = (state) => TONES[state] ?? "";

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * @param {any} dto one proposal of list_instruction_proposals
 * @returns {{ id: string, state: string, summary: string, skills: string[], files: number, task: { id: string, title: string } | null, session: { id: string, agent: string, mode: string } | null, createdAt: string, decidedAt: string, note: string }}
 */
export function proposalRow(dto) {
  return {
    id: dto.id ?? "",
    state: dto.state ?? "",
    summary: dto.summary ?? "",
    skills: list(dto.skills),
    files: Number.isInteger(dto.files) ? dto.files : 0,
    task: dto.task ? { id: dto.task.id ?? "", title: dto.task.title ?? "" } : null,
    session: dto.session ? { id: dto.session.id ?? "", agent: dto.session.agent ?? "", mode: dto.session.mode ?? "" } : null,
    createdAt: dto.created_at ?? "",
    decidedAt: dto.decided_at ?? "",
    note: dto.note ?? "",
  };
}

/** Open proposals first, then the decided ones, each group newest first. */
export function orderProposals(rows) {
  const newest = (a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt);
  const open = (r) => r.state === "open";
  return [...rows.filter(open).sort(newest), ...rows.filter((r) => !open(r)).sort(newest)];
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "just now", "25 min ago", "2 h ago", "yesterday", "3 days ago". */
export function agoText(then, now) {
  const ms = Math.max(0, now.getTime() - new Date(then).getTime());
  if (ms < MINUTE) return "just now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)} min ago`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)} h ago`;
  const days = Math.floor(ms / DAY);
  return days === 1 ? "yesterday" : `${days} days ago`;
}

/** "7 files · proposed 25 min ago", and once decided "· sent back yesterday". */
export function rowNote(row, now) {
  const parts = [count(row.files, "file"), `proposed ${agoText(row.createdAt, now)}`];
  if (row.decidedAt) parts.push(`${stateLabel(row.state).toLowerCase()} ${agoText(row.decidedAt, now)}`);
  return parts.join(" · ");
}

/** The proposing session: its agent and mode. */
export const sessionText = (row) => (row.session ? `${row.session.agent} · ${row.session.mode}` : "");
