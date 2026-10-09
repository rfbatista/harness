// Comments: plain-text notes the person writes on a task, one thread per
// task, oldest first. Mirrors internal/domain/ticket_resource.go's
// TicketComment. The body is plain text: the page escapes it, never links it.

import { runeCount } from "./resources.js";

/**
 * @typedef {object} Comment
 * @property {string} id
 * @property {string} ticketId
 * @property {string} projectId
 * @property {"person"} author
 * @property {string} body
 * @property {Date} createdAt
 * @property {Date} updatedAt
 */

/** Runes after the trim; the server refuses more. */
export const MAX_COMMENT = 10_000;
/** From here the composer shows how close the limit is. */
export const WARN_FROM = 9_000;

/** updated_at after created_at: the server keeps updated_at on a no-op edit. */
export const isEdited = (c) => c.updatedAt.getTime() > c.createdAt.getTime();

/** The body can be sent: something left after the trim, within the limit. */
export function canSend(text) {
  const n = runeCount((text ?? "").trim());
  return n > 0 && n <= MAX_COMMENT;
}

/** The counter's tone: none below 9,000, amber up to the limit, red at it. */
export function counterTone(length) {
  if (length >= MAX_COMMENT) return "danger";
  if (length >= WARN_FROM) return "attention";
  return "";
}

/** "9,412 / 10,000". */
export const counterWords = (length) => `${length.toLocaleString("en-US")} / ${MAX_COMMENT.toLocaleString("en-US")}`;

/** An edit that changes nothing after the trim sends nothing. */
export const isUnchanged = (comment, text) => (text ?? "").trim() === comment.body;
