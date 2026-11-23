import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { TopBarSourceError } from "../application/ports";
import { topBarJson, type TopBarJson, topBarFromJson } from "../testing/fixtures";
import { topBarSourceContract } from "../testing/top-bar-source-contract";
import { httpTopBarSource } from "./http-top-bar-source";
import { memoryTopBarSource } from "./memory-top-bar-source";

topBarSourceContract("HTTP adapter over a stubbed fetch", () => {
  const served = new Map<string, TopBarJson>();
  let down = false;
  let malformed: { raw: unknown } | null = null;
  const fetcher: FetchLike = async (url, init) => {
    if (init?.signal?.aborted) throw new DOMException("aborted", "AbortError");
    if (down) throw new TypeError("offline");
    const requested = new URL(url, "http://harness.test");
    assert.equal(requested.pathname, "/api/top_bar");
    const path = requested.searchParams.get("path") ?? "";
    if (!path.startsWith("/")) return json(400, { error: "path must be an absolute URL path", code: "INVALID_INPUT" });
    if (malformed) return json(200, malformed.raw);
    return json(200, served.get(path) ?? topBarJson({ path }));
  };
  return {
    source: httpTopBarSource(apiClient(fetcher)),
    serve: (model) => void served.set(model.path, model),
    breakNetwork: () => void (down = true),
    serveMalformed: (raw) => void (malformed = { raw }),
  };
});

topBarSourceContract("in-memory adapter", () => {
  const source = memoryTopBarSource((path) => topBarFromJson(topBarJson({ path })));
  return {
    source,
    serve: (model) => source.serve(model.path, topBarFromJson(model)),
    breakNetwork: () => source.failWith(new TopBarSourceError("NETWORK_ERROR", "offline")),
  };
});

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
