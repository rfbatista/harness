// The contract every ContextSource adapter keeps (Contract: Harness server ↔
// Web UI — session context). Run against the HTTP and in-memory adapters.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ContextSourceError, type ContextSource } from "../application/ports";
import { contextFromJson, contextJson, entryJson, pieceJson, type SessionContextJson } from "./fixtures";

export interface ContextSourceHarness {
  source: ContextSource;
  /** The server answers `context` for its session. */
  serve(context: SessionContextJson): void;
  /** The server has the full text of this piece. */
  servePiece(sessionId: string, entryId: string, pieceId: string, text: string): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
  /** The server answers `raw` (not a valid context) for the context. Adapters that read no wire have none. */
  serveMalformed?(raw: unknown): void;
}

const rejectsWith = (code: string) => (err: unknown) => err instanceof ContextSourceError && err.code === code;

export function contextSourceContract(name: string, make: () => ContextSourceHarness): void {
  describe(`ContextSource contract: ${name}`, () => {
    test("answers the session's context with its entries, usage and changes", async () => {
      const h = make();
      const json = contextJson({
        entries: [entryJson({ id: "e1" }), entryJson({ id: "e2", kind: "resume", at: "2026-11-24T09:12:00Z" })],
        usage: { tools: { Bash: { count: 3, last_at: "2026-11-23T21:00:00Z" } }, skills: {} },
        changes: [{ at: "2026-11-23T21:00:00Z", kind: "tool_first_used", subject: "Bash", detail: "first use" }],
      });
      h.serve(json);
      assert.deepEqual(await h.source.load("s1"), contextFromJson(json));
    });

    test("a session that started before context was recorded has no entries", async () => {
      const h = make();
      h.serve(contextJson({ recorded: false, entries: [] }));
      const context = await h.source.load("s1");
      assert.equal(context.recorded, false);
      assert.deepEqual(context.entries, []);
    });

    test("ignores fields it does not know", async () => {
      const h = make();
      h.serve({ ...contextJson(), future: 1 } as SessionContextJson);
      assert.equal((await h.source.load("s1")).sessionId, "s1");
    });

    test("a session the server does not know is SESSION_NOT_FOUND", async () => {
      const h = make();
      await assert.rejects(h.source.load("nope"), rejectsWith("SESSION_NOT_FOUND"));
    });

    test("a network failure is NETWORK_ERROR", async () => {
      const h = make();
      h.breakNetwork();
      await assert.rejects(h.source.load("s1"), rejectsWith("NETWORK_ERROR"));
    });

    test("answers the full text of a piece", async () => {
      const h = make();
      h.serve(contextJson({ entries: [entryJson({ id: "e1", instructions: [pieceJson({ id: "p1", truncated: true })] })] }));
      h.servePiece("s1", "e1", "p1", "the whole text");
      assert.equal(await h.source.piece("s1", "e1", "p1"), "the whole text");
    });

    test("a piece or entry that is not there is CONTEXT_PIECE_NOT_FOUND", async () => {
      const h = make();
      h.serve(contextJson());
      await assert.rejects(h.source.piece("s1", "e1", "missing"), rejectsWith("CONTEXT_PIECE_NOT_FOUND"));
    });

    test("a malformed answer is BAD_RESPONSE", async (t) => {
      const h = make();
      if (!h.serveMalformed) return t.skip("the adapter reads no wire");
      h.serveMalformed({ session_id: "s1", recorded: "yes" });
      await assert.rejects(h.source.load("s1"), rejectsWith("BAD_RESPONSE"));
    });
  });
}
