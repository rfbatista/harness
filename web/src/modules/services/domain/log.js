// A service's log as the viewer holds it: the last part of the log in lines,
// with the byte offset it is current to (contract: Harness server ↔ Web UI —
// services, sections 5 and 7). Pure: a chunk or a reset in, the next log out.

import { startedByView, whereLabel } from "./service.js";

/** The most lines kept in memory; the oldest go first. */
export const MAX_LINES = 5000;

const HARNESS = "[harness]";
const encoder = new TextEncoder();
const bytes = (text) => encoder.encode(text).length;

/**
 * @typedef {object} LogLine
 * @property {string} text
 * @property {"line" | "reset"} kind
 * @property {boolean} harness  a line the server wrote
 * @property {string} prefix  "[harness]" for a harness line
 * @property {string} body  the rest of the text
 * @property {boolean} complete  false for a last line that is still being written
 */

/**
 * @typedef {object} Log
 * @property {LogLine[]} lines
 * @property {number} end  the byte offset the log is current to
 * @property {string} runId
 * @property {boolean} earlier  lines before the first one shown exist
 */

let nextId = 0;
const line = (text, complete = true) => {
  const harness = text.startsWith(HARNESS);
  return { id: ++nextId, text, kind: "line", harness, prefix: harness ? HARNESS : "", body: harness ? text.slice(HARNESS.length) : text, complete };
};

/** @returns {Log} */
export const emptyLog = () => ({ lines: [], end: 0, runId: "", earlier: false });

/** Splits text into lines; a last piece without a newline is an open line. */
function split(text) {
  const parts = text.split("\n");
  const open = parts.pop() ?? "";
  return { done: parts, open };
}

/** @returns {Log} the log a read_service_log answer starts from */
export function fromRead(answer) {
  let text = answer.text;
  let cut = false;
  if (answer.offset > 0) {
    // The read began somewhere inside the run's log, so its first line may be cut.
    cut = true;
    const nl = text.indexOf("\n");
    if (nl >= 0) text = text.slice(nl + 1);
  }
  const { done, open } = split(text);
  const lines = done.map((t) => line(t));
  if (open !== "") lines.push(line(open, false));
  return capped({ lines, end: answer.next_offset, runId: answer.run_id, earlier: cut });
}

function capped(log) {
  if (log.lines.length <= MAX_LINES) return log;
  return { ...log, lines: log.lines.slice(log.lines.length - MAX_LINES), earlier: true };
}

/**
 * Appends a chunk that starts at the end of what is held. One that does not is a gap:
 * the log is returned as it was, and the viewer reads again from its own end.
 * @returns {{ log: Log, gap: boolean, added: number }}
 */
export function applyChunk(log, chunk) {
  if (chunk.offset !== log.end) return { log, gap: true, added: 0 };
  const lines = [...log.lines];
  let text = chunk.text;
  const last = lines.at(-1);
  if (last && !last.complete && last.kind === "line") {
    // The open line continues with the chunk's first piece.
    lines.pop();
    text = last.text + text;
  }
  const { done, open } = split(text);
  const fresh = done.map((t) => line(t));
  if (open !== "") fresh.push(line(open, false));
  const next = capped({ ...log, lines: [...lines, ...fresh], end: log.end + bytes(chunk.text) });
  return { log: next, gap: false, added: done.length };
}

/**
 * What a re-read from the end of the held log found. A read that starts where the log ends continues it
 * (and the log is current to the answer's next_offset); a read that starts elsewhere (another run, or the
 * server's 5 MB cap moved past what is held) replaces it.
 * @returns {{ log: Log, replaced: boolean, added: number }}
 */
export function rereadLog(log, answer) {
  const sameRun = log.runId === "" || answer.run_id === log.runId;
  if (!sameRun || answer.offset !== log.end) return { log: fromRead(answer), replaced: true, added: 0 };
  const next = applyChunk(log, { offset: log.end, text: answer.text, run_id: answer.run_id });
  return { log: { ...next.log, end: answer.next_offset }, replaced: false, added: next.added };
}

/** A new run began: the log was cleared on the server. `at` is the time the viewer shows. */
export function applyReset(log, reset, at) {
  const text = `New run started ${at} · the log was cleared`;
  return { lines: [{ id: ++nextId, text, kind: "reset", harness: false, prefix: "", body: text, complete: true }], end: 0, runId: reset.run_id, earlier: false };
}

/** The note over the log when its start is not shown. */
export function tailNote(log) {
  return log.earlier ? "Showing the last 64 KB. The log keeps the last 5 MB of this run." : "";
}

const hhmm = (iso) => {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
};

/** "Run started 14:02 · by you · main checkout @ main"; "" with no run. */
export function runLine(run) {
  if (!run) return "";
  const by = startedByView(run);
  const who = by.sessionId ? `by ${run.started_by.agent}` : "by you";
  return ["Run started " + hhmm(run.started_at), who, whereLabel(run)].filter(Boolean).join(" · ");
}
