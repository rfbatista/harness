import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { ContextSourceError } from "../application/ports";
import { contextJson } from "../testing/fixtures";
import { httpContextSource } from "./http-context-source";

const reply = (status: number, body: string) => new Response(body, { status, headers: { "Content-Type": "application/json" } });

const sourceAnswering = (answer: (url: string) => Response) => {
  const urls: string[] = [];
  const fetcher: FetchLike = async (url) => {
    urls.push(url);
    return answer(url);
  };
  return { urls, source: httpContextSource(apiClient(fetcher)) };
};

const code = (c: string) => (err: unknown) => err instanceof ContextSourceError && err.code === c;

describe("HTTP context source: the server's answers", () => {
  test("asks for the session's context and for a piece at the contract's paths, encoding ids", async () => {
    const { urls, source } = sourceAnswering((url) => (url.endsWith("/context") ? reply(200, JSON.stringify(contextJson({ session_id: "a/b" }))) : reply(200, '{"text":"t"}')));
    await source.load("a/b");
    await source.piece("a/b", "e 1", "p/1");
    assert.deepEqual(urls, ["/api/sessions/a%2Fb/context", "/api/sessions/a%2Fb/context/entries/e%201/pieces/p%2F1"]);
  });

  test("the contract's three 404 codes come through as they are", async () => {
    for (const c of ["SESSION_NOT_FOUND", "CONTEXT_ENTRY_NOT_FOUND", "CONTEXT_PIECE_NOT_FOUND"]) {
      const { source } = sourceAnswering(() => reply(404, JSON.stringify({ error: "no", code: c })));
      await assert.rejects(source.piece("s1", "e1", "p1"), code(c));
    }
  });

  test("an unknown error code is UNKNOWN, and keeps the server's words", async () => {
    const { source } = sourceAnswering(() => reply(403, JSON.stringify({ error: "not yours", code: "FORBIDDEN" })));
    await assert.rejects(source.load("s1"), (e) => e instanceof ContextSourceError && e.code === "UNKNOWN" && e.message === "not yours");
  });

  test("a server error without the error body is UNKNOWN, not a crash", async () => {
    const { source } = sourceAnswering(() => reply(500, "<html>boom</html>"));
    await assert.rejects(source.load("s1"), code("UNKNOWN"));
  });

  test("a 200 that is not JSON, or not a context, is BAD_RESPONSE", async () => {
    await assert.rejects(sourceAnswering(() => reply(200, "<html>login</html>")).source.load("s1"), code("BAD_RESPONSE"));
    await assert.rejects(sourceAnswering(() => reply(200, "[]")).source.load("s1"), code("BAD_RESPONSE"));
  });

  test("a piece answer without text is BAD_RESPONSE", async () => {
    await assert.rejects(sourceAnswering(() => reply(200, "{}")).source.piece("s1", "e1", "p1"), code("BAD_RESPONSE"));
  });

  test("a bad answer's message names the DTO and the field", async () => {
    const { source } = sourceAnswering(() => reply(200, JSON.stringify({ ...contextJson(), recorded: 1 })));
    await assert.rejects(source.load("s1"), (e) => e instanceof ContextSourceError && e.message.includes("recorded"));
  });

  test("an aborted request is not hidden as a failure of the server", async () => {
    const fetcher: FetchLike = async () => {
      throw new DOMException("aborted", "AbortError");
    };
    await assert.rejects(httpContextSource(apiClient(fetcher)).load("s1"), (e) => (e as Error).name === "AbortError");
  });

  test("reads times as dates", async () => {
    const { source } = sourceAnswering(() => reply(200, JSON.stringify(contextJson({ usage: { tools: { Bash: { count: 2, last_at: "2026-11-23T21:00:00Z" } }, skills: {} } }))));
    const ctx = await source.load("s1");
    assert.equal(ctx.usage.tools.get("Bash")!.lastAt.toISOString(), "2026-11-23T21:00:00.000Z");
    assert.equal(ctx.entries[0]!.at.toISOString(), "2026-11-23T20:49:00.000Z");
  });
});
