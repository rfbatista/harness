// The instruction-file proposals of a repository, in words: what an agent
// proposed (ADR 0092) as the Instructions & rules tab lists it. Pure.

import { count } from "../../../shared/presentation/format.js";
import { diffRows } from "../../code/domain/rows.js";
import { lineDiff } from "./instructionFiles.js";

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

// ── a proposal's files and their diffs ──────────────────────────────────────

const CONTEXT = 2;

/**
 * @param {any} dto one file of get_instruction_proposal
 * @returns {{ path: string, op: string, baseVersion: number, currentVersion: number, stale: boolean, baseContent: string, content: string }}
 */
export function proposalFile(dto) {
  const baseVersion = Number.isInteger(dto.base_version) ? dto.base_version : 0;
  const currentVersion = Number.isInteger(dto.current_version) ? dto.current_version : 0;
  return {
    path: dto.path ?? "",
    op: dto.op ?? "change",
    baseVersion,
    currentVersion,
    stale: dto.stale === true || currentVersion !== baseVersion,
    baseContent: dto.base_content ?? "",
    content: dto.content ?? "",
  };
}

/** The files' line diff as the code view's rows: only changed lines with two lines of context, in hunks; a create or a remove is whole. */
export function fileDiff(file) {
  const lines = [];
  let o = 0;
  let n = 0;
  for (const d of lineDiff(file.baseContent, file.content)) {
    if (d.type === "eq") lines.push({ kind: "context", old: ++o, new: ++n, text: d.text, noNewline: false });
    else if (d.type === "del") lines.push({ kind: "del", old: ++o, new: null, text: d.text, noNewline: false });
    else lines.push({ kind: "add", old: null, new: ++n, text: d.text, noNewline: false });
  }
  const added = lines.filter((l) => l.kind === "add").length;
  const deleted = lines.filter((l) => l.kind === "del").length;
  const whole = file.op !== "change";
  const rows = diffRows(whole ? (lines.length ? [wholeHunk(lines)] : []) : hunksOf(lines)).filter((r) => !whole || r.type !== "hunk");
  return { rows, added, deleted, digits: Math.max(String(Math.max(o, n, 1)).length, 2) };
}

function wholeHunk(lines) {
  return { oldStart: 1, oldLines: lines.length, newStart: 1, newLines: lines.length, header: "", lines };
}

/** The changed lines with CONTEXT lines around them, as hunks. */
function hunksOf(lines) {
  const keep = lines.map(() => false);
  lines.forEach((l, i) => {
    if (l.kind === "context") return;
    for (let d = -CONTEXT; d <= CONTEXT; d++) if (i + d >= 0 && i + d < lines.length) keep[i + d] = true;
  });
  const hunks = [];
  let cur = null;
  lines.forEach((l, i) => {
    if (!keep[i]) {
      cur = null;
      return;
    }
    if (!cur) {
      cur = [];
      hunks.push(cur);
    }
    cur.push(l);
  });
  return hunks.map((h) => {
    const first = (side) => h.find((l) => l[side] !== null)?.[side] ?? 0;
    return {
      oldStart: first("old"),
      oldLines: h.filter((l) => l.kind !== "add").length,
      newStart: first("new"),
      newLines: h.filter((l) => l.kind !== "del").length,
      header: "",
      lines: h,
    };
  });
}

const OP_TONES = { create: "success", change: "", remove: "danger" };
const OP_MARKS = { create: ["added", "A"], change: ["modified", "M"], remove: ["deleted", "D"] };

/** What a file's head says: its op, its mark and tone, and either the stale marker (an open proposal only) or the version it is against. */
export function fileHead(file, state) {
  const stale = file.stale && state === "open";
  return {
    op: file.op,
    opTone: OP_TONES[file.op] ?? "",
    change: OP_MARKS[file.op]?.[0] ?? "modified",
    mark: OP_MARKS[file.op]?.[1] ?? "M",
    stale,
    staleText: stale ? `stale · v${file.baseVersion} read, v${file.currentVersion} saved` : "",
    againstText: !stale && file.op !== "create" ? `against v${file.baseVersion}` : "",
  };
}

/** "4 create · 2 change · 1 remove". */
export function opSummary(files) {
  return ["create", "change", "remove"]
    .map((op) => [op, files.filter((f) => f.op === op).length])
    .filter(([, n]) => n > 0)
    .map(([op, n]) => `${n} ${op}`)
    .join(" · ");
}

/** The notice over the decision row when files changed since the draft read them. */
export function staleCountText(n) {
  if (n === 0) return "";
  return n === 1 ? "1 file changed since the draft read it." : `${n} files changed since the draft read them.`;
}

export const approveLabel = (n) => `Approve ${count(n, "file")}`;

/** The stale paths an INSTRUCTION_CHANGED refusal listed, each with the versions read and saved when the pane knows them. */
export function refusedFiles(paths, files) {
  return paths.map((path) => {
    const f = files.find((x) => x.path === path);
    return { path, text: f && f.stale ? `v${f.baseVersion} read, v${f.currentVersion} saved` : "" };
  });
}

export function refusalTitle(n) {
  return n === 1
    ? "Approval refused: 1 file was saved since the draft read it. Nothing was saved."
    : `Approval refused: ${n} files were saved since the draft read them. Nothing was saved.`;
}

/** What an approval saved. */
export function approvedText(files) {
  const saved = files.filter((f) => f.op !== "remove").length;
  const removed = files.length - saved;
  const parts = [];
  if (saved > 0) parts.push(saved === 1 ? "1 file saved as a new version" : `${saved} files saved as new versions`);
  if (removed > 0) parts.push(`${removed} removed`);
  return `${parts.join(", ")}. Sessions you start from now on receive them.`;
}

/** Why a send back cannot go: it needs a note. "" when it can. */
export const sendBackNoteError = (note) => (note.trim() === "" ? "A note is required: tell the session what to change." : "");
