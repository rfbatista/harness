// What the History tab shows for one transcript entry: a line in the design
// system's transcript block. Every string here is an agent's or a person's
// text; the markup binds it with x-text, never as HTML.

/**
 * @typedef {object} HistoryLine
 * @property {string} cursor
 * @property {"user" | "agent" | "tool" | "error" | "system"} kind  the block's data-kind
 * @property {string} mark   the glyph in the mark column
 * @property {string} time   HH:MM, "" without a time
 * @property {string} stamp  the full time, for the tooltip and datetime
 * @property {boolean} fold  a tool call or result: one line until opened
 * @property {string} text   the whole text (a fold's detail)
 * @property {string} tool   the tool's name on a fold
 * @property {string} gist   a fold's one line
 * @property {string} meta   a fold's size, "12 lines"
 * @property {boolean} cut   the server cut the text at 16 KiB
 */

const MARKS = { user: "›", assistant: "⏺", system: "·", tool_use: "⌁", tool_result: "↳" };

const two = (n) => String(n).padStart(2, "0");

/** @param {import("../domain/transcript.js").TranscriptEntry} entry @returns {HistoryLine} */
export function historyLine(entry) {
  const fold = entry.kind === "tool_use" || entry.kind === "tool_result";
  const error = entry.kind === "tool_result" && entry.isError;
  const lines = entry.text.split("\n");
  const first = lines.find((l) => l.trim() !== "")?.trim() ?? "";
  return {
    cursor: entry.cursor,
    kind: error ? "error" : fold ? "tool" : entry.kind === "assistant" ? "agent" : entry.kind,
    mark: error ? "✕" : MARKS[entry.kind],
    time: entry.at ? `${two(entry.at.getHours())}:${two(entry.at.getMinutes())}` : "",
    stamp: entry.at ? entry.at.toLocaleString() : "",
    fold,
    text: entry.text,
    tool: fold ? entry.tool : "",
    gist: fold ? first || (entry.kind === "tool_result" ? "no output" : "") : "",
    meta: fold && lines.length > 1 ? `${lines.length} lines` : "",
    cut: entry.truncated,
  };
}
