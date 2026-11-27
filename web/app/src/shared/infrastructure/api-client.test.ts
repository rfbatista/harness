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
  const body = await apiClient(fetcher).get("/top_bar", { path: "/projects/p 1" });
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
  await apiClient(fetcher).post("/notifications/read_all", { toJSON: () => ({ a: 1 }) });
  assert.equal(seen?.method, "POST");
  assert.equal(seen?.body, '{"a":1}');
});

test("an error body that is not the API's still becomes an ApiError, by status", async () => {
  const fetcher: FetchLike = async () => new Response(JSON.stringify({ message: "nope" }), { status: 500, statusText: "Server Error" });
  await assert.rejects(apiClient(fetcher).get("/x"), (err: unknown) => err instanceof ApiError && err.code === "HTTP_500" && err.message === "Server Error");
});

test("a 200 whose body is not JSON is BAD_RESPONSE", async () => {
  const fetcher: FetchLike = async () => new Response("<html>sign in</html>", { status: 200 });
  await assert.rejects(apiClient(fetcher).get("/x"), (err: unknown) => err instanceof ApiError && err.code === "BAD_RESPONSE");
});

test("a patch sends its JSON body with PATCH, and a delete sends DELETE", async () => {
  const seen: RequestInit[] = [];
  const urls: string[] = [];
  const fetcher: FetchLike = async (url, init) => {
    urls.push(url);
    seen.push(init ?? {});
    return json(200, { ok: true });
  };
  const api = apiClient(fetcher);
  assert.deepEqual(await api.patch("/projects/p/agent-system", { toJSON: () => ({ "guard.retry_limit": 5 }) }), { ok: true });
  await api.delete("/projects/p/bench/cases/c1");
  assert.deepEqual(urls, ["/api/projects/p/agent-system", "/api/projects/p/bench/cases/c1"]);
  assert.equal(seen[0]?.method, "PATCH");
  assert.equal(seen[0]?.body, '{"guard.retry_limit":5}');
  assert.equal(seen[1]?.method, "DELETE");
});

test("a refusal whose body is not the API's error shape keeps its body on the ApiError", async () => {
  const body = { errors: [{ field: "guard.retry_limit", code: "OUT_OF_RANGE", message: "1 to 10" }] };
  const fetcher: FetchLike = async () => json(422, body);
  await assert.rejects(apiClient(fetcher).patch("/x"), (err: unknown) => err instanceof ApiError && err.status === 422 && JSON.stringify(err.body) === JSON.stringify(body));
});
