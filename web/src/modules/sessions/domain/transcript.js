// A session's saved conversation, as the claude CLI wrote it (the contract
// "Harness server ↔ Web UI — agent session history", T1–T6): entries oldest
// first, read a page at a time with opaque cursors. Read-only.

import { Codes } from "../../../shared/domain/errors.js";

/** The entry kinds the server sends; others are skipped. */
export const ENTRY_KINDS = Object.freeze(["user", "assistant", "tool_use", "tool_result", "system"]);

/** How many entries one page asks for (the server allows 1–500). */
export const TRANSCRIPT_PAGE = 200;

/**
 * @typedef {object} TranscriptEntry
 * @property {string} cursor   opaque, stable for the file's life
 * @property {Date | null} at  when the CLI recorded it
 * @property {"user" | "assistant" | "tool_use" | "tool_result" | "system"} kind
 * @property {string} text     as written; a tool_use's is a one-line summary of the call
 * @property {string} tool     the tool's name on tool_use / tool_result, else ""
 * @property {boolean} isError a tool_result that failed
 * @property {boolean} truncated the server cut the text at 16 KiB
 * @property {string} html     user / assistant: the text as Markdown, rendered
 *                             and made safe by the server (T7); "" otherwise
 *
 * @typedef {object} TranscriptPage
 * @property {TranscriptEntry[]} entries  always oldest → newest
 * @property {string} firstCursor  entries[0]'s, "" when empty
 * @property {string} lastCursor   the last entry's, "" when empty
 * @property {boolean} hasOlder    more before entries[0]
 * @property {boolean} hasNewer    more after the last entry right now
 */

/**
 * The server would not take a cursor it gave (only the exact returned form
 * is accepted, and a rewritten transcript has new ones): start again from
 * the newest page rather than show an error.
 */
export function isStaleCursor(err) {
  return err?.code === Codes.INVALID_INPUT;
}

/**
 * The session has no saved conversation to show (yet): nothing is wrong, the
 * view is empty. A 404 without a code (a server that does not serve the
 * route yet) reads the same.
 */
export function isTranscriptMissing(err) {
  const code = err?.code;
  if (code === Codes.SESSION_TRANSCRIPT_MISSING || code === Codes.SESSION_NOT_FOUND) return true;
  return err?.status === 404 && (code === Codes.UNKNOWN || code === Codes.BAD_RESPONSE);
}
