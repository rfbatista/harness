import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { ContextSourceError } from "../application/ports";
import { contextFromJson, type SessionContextJson } from "../testing/fixtures";
import { contextSourceContract } from "../testing/context-source-contract";
import { httpContextSource } from "./http-context-source";
import { memoryContextSource } from "./memory-context-source";

contextSourceContract("HTTP adapter over a stubbed fetch", () => {
  const contexts = new Map<string, SessionContextJson>();
  const pieces = new Map<string, string>();
  let down = false;
  let malformed: { raw: unknown } | null = null;
  const fetcher: FetchLike = async (url, init) => {
    if (init?.signal?.aborted) throw new DOMException("aborted", "AbortError");
    if (down) throw new TypeError("offline");
    const path = new URL(url, "http://harness.test").pathname;
    const piece = /^\/api\/sessions\/([^/]+)\/context\/entries\/([^/]+)\/pieces\/([^/]+)$/.exec(path);
    if (piece) {
      const text = pieces.get(`${piece[1]}/${piece[2]}/${piece[3]}`);
      return text === undefined ? json(404, { error: "no such piece", code: "CONTEXT_PIECE_NOT_FOUND" }) : json(200, { text });
    }
    const context = /^\/api\/sessions\/([^/]+)\/context$/.exec(path);
    if (!context) return json(404, { error: "no route", code: "NOT_FOUND" });
    if (malformed) return json(200, malformed.raw);
    const served = contexts.get(context[1]!);
    return served ? json(200, served) : json(404, { error: "no such session", code: "SESSION_NOT_FOUND" });
  };
  return {
    source: httpContextSource(apiClient(fetcher)),
    serve: (context) => void contexts.set(context.session_id, context),
    servePiece: (session, entry, piece, text) => void pieces.set(`${session}/${entry}/${piece}`, text),
    breakNetwork: () => void (down = true),
    serveMalformed: (raw) => void (malformed = { raw }),
  };
});

contextSourceContract("in-memory adapter", () => {
  const source = memoryContextSource();
  return {
    source,
    serve: (context) => source.serve(contextFromJson(context)),
    servePiece: (session, entry, piece, text) => source.servePiece(session, entry, piece, text),
    breakNetwork: () => source.failWith(new ContextSourceError("NETWORK_ERROR", "offline")),
  };
});

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
