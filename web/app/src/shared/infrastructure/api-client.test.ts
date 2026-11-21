import { test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, ApiError, type FetchLike } from "./api-client";

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

test("a 200 gives the body, and the query goes in the URL", async () => {
  const urls: string[] = [];
  const fetcher: FetchLike = async (url) => {
    urls.push(url);
    return json(200, { ok: true });
  };
  const body = await apiClient(fetcher).get<{ ok: boolean }>("/top_bar", { path: "/projects/p 1" });
  assert.deepEqual(body, { ok: true });
  assert.deepEqual(urls, ["/api/top_bar?path=%2Fprojects%2Fp+1"]);
});

test("an API error body becomes an ApiError with its code", async () => {
  const fetcher: FetchLike = async () => json(400, { error: "path is required", code: "INVALID_INPUT" });
  await assert.rejects(apiClient(fetcher).get("/top_bar"), (err: unknown) => err instanceof ApiError && err.code === "INVALID_INPUT" && err.message === "path is required" && err.status === 400);
});

test("a failed fetch becomes a NETWORK_ERROR", async () => {
  const fetcher: FetchLike = async () => {
    throw new TypeError("offline");
  };
  await assert.rejects(apiClient(fetcher).post("/x"), (err: unknown) => err instanceof ApiError && err.code === "NETWORK_ERROR");
});

test("a post sends its JSON body", async () => {
  let seen: RequestInit | undefined;
  const fetcher: FetchLike = async (_url, init) => {
    seen = init;
    return new Response(null, { status: 204 });
  };
  await apiClient(fetcher).post("/notifications/read_all", { a: 1 });
  assert.equal(seen?.method, "POST");
  assert.equal(seen?.body, '{"a":1}');
});
