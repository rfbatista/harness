// A service's log as the panel shows it: lines with a tone.

export type LineTone = "plain" | "harness" | "error";

export interface LogLine {
  readonly text: string;
  readonly tone: LineTone;
}

const ERROR = /\b(error|fatal|panic|exception|failed)\b/i;

export function toneOf(text: string): LineTone {
  if (text.startsWith("[harness]")) return "harness";
  return ERROR.test(text) ? "error" : "plain";
}

export interface LogRead {
  readonly text: string;
  readonly offset: number;
  readonly nextOffset: number;
  readonly runId: string;
}

export type LogEvent = { readonly kind: "chunk"; readonly runId: string; readonly offset: number; readonly text: string } | { readonly kind: "reset"; readonly runId: string };

export interface LogState {
  readonly lines: readonly LogLine[];
  /** A trailing part of a line that has not ended yet. */
  readonly partial: string;
  readonly end: number;
  readonly runId: string;
}

export const EMPTY_LOG: LogState = { lines: [], partial: "", end: 0, runId: "" };

function append(state: LogState, text: string, end: number): LogState {
  const parts = (state.partial + text).split("\n");
  const partial = parts.pop() ?? "";
  return { ...state, lines: [...state.lines, ...parts.map((t) => ({ text: t, tone: toneOf(t) }))], partial, end };
}

/** The log a read shows. */
export const fromRead = (read: LogRead): LogState => append({ ...EMPTY_LOG, runId: read.runId }, read.text, read.nextOffset);

const bytes = (text: string): number => new TextEncoder().encode(text).length;

/** A chunk extends the log when it starts where the log ends; one entirely behind is already in it; one ahead or overlapping is a gap (reread). */
export function applyChunk(state: LogState, event: Extract<LogEvent, { kind: "chunk" }>): { state: LogState; gap: boolean } {
  if (state.runId !== "" && event.runId !== state.runId) return { state, gap: true };
  const end = event.offset + bytes(event.text);
  if (end <= state.end) return { state, gap: false };
  if (event.offset !== state.end) return { state, gap: true };
  return { state: append(state, event.text, end), gap: false };
}
