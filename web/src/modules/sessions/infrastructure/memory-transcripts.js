// TranscriptGateway in memory, for tests and web/dev: each session's entries,
// oldest first, paged with cursors as the server pages them. A session
// without an entry list has no saved conversation.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/**
 * @param {{ transcripts?: Record<string, Partial<import("../domain/transcript.js").TranscriptEntry>[]> }} [world]
 */
export function memoryTranscripts(world = {}) {
  /** @type {Map<string, import("../domain/transcript.js").TranscriptEntry[]>} */
  const files = new Map();
  let seq = 0;
  /** Every page asked for, in order: what a test checks the polling against. */
  const calls = [];
  let failure = null;

  const entry = (fields) =>
    Object.freeze({
      cursor: fields.cursor ?? `c${String(++seq).padStart(6, "0")}`,
      at: fields.at ?? null,
      kind: fields.kind ?? "assistant",
      text: fields.text ?? "",
      tool: fields.tool ?? "",
      isError: fields.isError ?? false,
      truncated: fields.truncated ?? false,
      html: fields.html ?? "",
    });

  for (const [id, list] of Object.entries(world.transcripts ?? {})) files.set(id, list.map(entry));

  const position = (list, cursor) => {
    const at = list.findIndex((e) => e.cursor === cursor);
    if (at === -1) throw new StructuredError(Codes.INVALID_INPUT, `unknown cursor ${cursor}`, 400);
    return at;
  };

  /** @type {import("../domain/ports.js").TranscriptGateway} */
  const gateway = {
    async page(sessionId, { before, after, limit = 200 } = {}) {
      calls.push({ sessionId, before, after, limit });
      if (failure) {
        const err = failure;
        failure = null;
        throw err;
      }
      if (before && after) throw new StructuredError(Codes.INVALID_INPUT, "before and after together", 400);
      if (!Number.isInteger(limit) || limit < 1 || limit > 500) throw new StructuredError(Codes.INVALID_INPUT, "limit is 1–500", 400);
      const list = files.get(sessionId);
      if (!list) throw new StructuredError(Codes.SESSION_TRANSCRIPT_MISSING, "no saved conversation for this session yet", 404);
      let start;
      let end;
      if (before) {
        end = position(list, before);
        start = Math.max(0, end - limit);
      } else if (after) {
        start = position(list, after) + 1;
        end = Math.min(list.length, start + limit);
      } else {
        end = list.length;
        start = Math.max(0, end - limit);
      }
      const entries = list.slice(start, end);
      return Object.freeze({
        entries,
        firstCursor: entries[0]?.cursor ?? "",
        lastCursor: entries.at(-1)?.cursor ?? "",
        hasOlder: start > 0,
        hasNewer: end < list.length,
      });
    },
  };

  return {
    gateway,
    calls,
    /** The CLI wrote more of the session's conversation (creating it if it had none). */
    append(sessionId, ...fields) {
      const list = files.get(sessionId) ?? [];
      const added = fields.map(entry);
      files.set(sessionId, [...list, ...added]);
      return added;
    },
    /** The next page fails with err. */
    failNext(err) {
      failure = err;
    },
  };
}
