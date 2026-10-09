// Comments: plain-text notes on a task, one thread per task, oldest first,
// written by the person or by a session working on the task. Mirrors
// internal/domain/ticket_resource.go's TicketComment. The body is plain text:
// the page escapes it, never links it.

import { runeCount } from "./resources.js";

/**
 * @typedef {object} Comment
 * @property {string} id
 * @property {string} ticketId
 * @property {string} projectId
 * @property {"person" | "session"} author
 * @property {string} sessionId  "" for the person
 * @property {string} agentName  "" for the person, or a session with no agent
 * @property {string} body
 * @property {Date} createdAt
 * @property {Date} updatedAt
 */

/** Runes after the trim; the server refuses more. */
export const MAX_COMMENT = 10_000;
/** From here the composer shows how close the limit is. */
export const WARN_FROM = 9_000;

/** A session wrote it, not the person. */
export const isBySession = (c) => c.author === "session";

/** Who wrote it, in words: "You", the agent's name, or "Agent session" when it has none. */
export const authorName = (c) => (isBySession(c) ? c.agentName || "Agent session" : "You");

/** Only the person's own comments can be edited; a session's can only be deleted. */
export const canEdit = (c) => !isBySession(c);

/** updated_at after created_at: the server keeps updated_at on a no-op edit. */
export const isEdited = (c) => c.updatedAt.getTime() > c.createdAt.getTime();

/** The body can be sent: something left after the trim, within the limit. */
export function canSend(text) {
  const n = runeCount((text ?? "").trim());
  return n > 0 && n <= MAX_COMMENT;
}

/** The counter's tone: none below 9,000, amber up to the limit, red past it (it cannot be sent). */
export function counterTone(length) {
  if (length > MAX_COMMENT) return "danger";
  if (length >= WARN_FROM) return "attention";
  return "";
}

/** "9,412 / 10,000". */
export const counterWords = (length) => `${length.toLocaleString("en-US")} / ${MAX_COMMENT.toLocaleString("en-US")}`;

/** An edit that changes nothing after the trim sends nothing. */
export const isUnchanged = (comment, text) => (text ?? "").trim() === comment.body;
