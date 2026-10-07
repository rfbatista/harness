import { Codes } from "../domain/errors.js";
import { fakeFetch, jsonResponse } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { apiClient } from "./api.js";

file("shared/infrastructure/api");

test("GET sends query params and skips empty ones", async () => {
  const { fetch, calls } = fakeFetch(() => jsonResponse(200, { ok: true }));
  const api = apiClient({ base: "/api", fetch });
  assert.deepEqual(await api.get("/sessions", { project_id: "p 1", agent_id: "", ticket_id: undefined }), { ok: true });
  assert.equal(calls[0].url, "/api/sessions?project_id=p+1");
  assert.equal(calls[0].init.method, "GET");
});

test("POST sends a JSON body and accepts an empty 202", async () => {
  const { fetch, calls } = fakeFetch(() => new Response(null, { status: 202 }));
  const api = apiClient({ base: "/api", fetch });
  assert.equal(await api.post("/sessions/s1/messages", { text: "hi" }), null);
  assert.equal(calls[0].init.body, '{"text":"hi"}');
  assert.equal(calls[0].init.headers["Content-Type"], "application/json");
});

test("an error response becomes a StructuredError with the server's code", async () => {
  const { fetch } = fakeFetch(() => jsonResponse(404, { error: "session s9 not found", code: "SESSION_NOT_FOUND" }));
  const err = await assert.rejects(apiClient({ base: "", fetch }).get("/sessions/s9"), Codes.SESSION_NOT_FOUND);
  assert.equal(err.message, "session s9 not found");
  assert.equal(err.status, 404);
});

test("unreadable errors are BAD_RESPONSE, uncoded ones UNKNOWN, an unreachable server NETWORK", async () => {
  const plain = fakeFetch(() => new Response("boom", { status: 500, statusText: "Internal Server Error" }));
  await assert.rejects(apiClient({ base: "", fetch: plain.fetch }).get("/x"), Codes.BAD_RESPONSE);

  const nocode = fakeFetch(() => jsonResponse(500, { error: "internal" }));
  await assert.rejects(apiClient({ base: "", fetch: nocode.fetch }).get("/x"), Codes.UNKNOWN);

  const down = fakeFetch(() => Promise.reject(new TypeError("Failed to fetch")));
  await assert.rejects(apiClient({ base: "", fetch: down.fetch }).get("/x"), Codes.NETWORK);
});

test("an error's details travel on the StructuredError; errors without them have none", async () => {
  const sessions = [{ id: "s1", ticket_id: "t1", agent: "go-developer" }];
  const refused = fakeFetch(() =>
    jsonResponse(409, { error: "project has 1 running session", code: "PROJECT_HAS_RUNNING_SESSIONS", details: { sessions } }),
  );
  const err = await assert.rejects(apiClient({ base: "", fetch: refused.fetch }).post("/delete_project", {}), Codes.PROJECT_HAS_RUNNING_SESSIONS);
  assert.deepEqual(err.details, { sessions });

  const plain = fakeFetch(() => jsonResponse(404, { error: "gone", code: "PROJECT_NOT_FOUND" }));
  const other = await assert.rejects(apiClient({ base: "", fetch: plain.fetch }).get("/x"), Codes.PROJECT_NOT_FOUND);
  assert.equal(other.details, undefined);
});
