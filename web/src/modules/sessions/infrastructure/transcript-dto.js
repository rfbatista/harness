// The wire format of GET /api/sessions/{id}/transcript (the contract
// "Harness server ↔ Web UI — agent session history") and its mapping to
// domain entries. Entries of a kind this client does not know are skipped.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { ENTRY_KINDS } from "../domain/transcript.js";
import { optionalDate } from "./dto.js";

/** @returns {import("../domain/transcript.js").TranscriptEntry | null} */
export function toTranscriptEntry(dto) {
  if (!dto || typeof dto.cursor !== "string" || dto.cursor === "") bad("transcript entry without a cursor");
  if (!ENTRY_KINDS.includes(dto.kind)) return null;
  return Object.freeze({
    cursor: dto.cursor,
    at: optionalDate(dto.at, `transcript entry ${dto.cursor}: at`),
    kind: dto.kind,
    text: typeof dto.text === "string" ? dto.text : "",
    tool: typeof dto.tool === "string" ? dto.tool : "",
    isError: dto.is_error === true,
    truncated: dto.truncated === true,
    html: typeof dto.html === "string" ? dto.html : "",
  });
}

/** @returns {import("../domain/transcript.js").TranscriptPage} */
export function toTranscriptPage(body) {
  if (!body || !Array.isArray(body.entries)) bad("expected {entries: [...]}");
  return Object.freeze({
    entries: body.entries.map(toTranscriptEntry).filter((e) => e !== null),
    firstCursor: typeof body.first_cursor === "string" ? body.first_cursor : "",
    lastCursor: typeof body.last_cursor === "string" ? body.last_cursor : "",
    hasOlder: body.has_older === true,
    hasNewer: body.has_newer === true,
  });
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected transcript data: ${detail}.`);
}
