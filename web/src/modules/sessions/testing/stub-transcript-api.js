// A stand-in for the harness server's transcript route, backed by the memory
// transcripts: a fetch that speaks the contract's wire format. The real
// transcriptGateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryTranscripts } from "../infrastructure/memory-transcripts.js";

const STATUS = { SESSION_TRANSCRIPT_MISSING: 404, SESSION_NOT_FOUND: 404, INVALID_INPUT: 400 };

/** The wire shape of an entry. */
export const entryDTO = (e) => ({
  cursor: e.cursor,
  at: e.at ? e.at.toISOString() : "",
  kind: e.kind,
  text: e.text,
  tool: e.tool,
  is_error: e.isError,
  truncated: e.truncated,
  html: e.html,
});

export function stubTranscriptApi(world) {
  const memory = memoryTranscripts(world);

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const route = url.pathname.match(/^\/api\/sessions\/([^/]+)\/transcript$/);
    if ((init.method ?? "GET") !== "GET" || !route) return jsonResponse(404, { error: `no route ${url.pathname}` });
    const q = url.searchParams;
    try {
      const page = await memory.gateway.page(decodeURIComponent(route[1]), {
        before: q.get("before") ?? undefined,
        after: q.get("after") ?? undefined,
        limit: q.has("limit") ? Number(q.get("limit")) : undefined,
      });
      return jsonResponse(200, {
        entries: page.entries.map(entryDTO),
        first_cursor: page.firstCursor,
        last_cursor: page.lastCursor,
        has_older: page.hasOlder,
        has_newer: page.hasNewer,
      });
    } catch (err) {
      return jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  return { fetch, memory };
}
