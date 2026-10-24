import { StructuredError } from "../../../shared/domain/errors.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { stepGateway } from "./step-gateway.js";

file("writinghelper/infrastructure/step-gateway");

const enc = new TextEncoder();

/** A 200 event-stream whose body arrives in the given chunks. */
function streamOf(...chunks) {
  const body = new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(enc.encode(c));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

const request = { project_id: "p1", step: "draft", field: { kind: "task_description", text: "x" } };

test("a step posts its JSON and returns the final result", async () => {
  const { fetch, calls } = fakeFetch(() => streamOf('event: result\ndata: {"text":"Whole draft"}\n\n'));
  const result = await stepGateway({ base: "/api", fetch }).runStep(request);
  assert.deepEqual(result, { text: "Whole draft" });
  assert.equal(calls[0].url, "/api/run_writing_helper_step");
  assert.equal(calls[0].init.method, "POST");
  assert.equal(calls[0].init.body, JSON.stringify(request));
});

test("deltas reach the caller as they arrive, even when a chunk splits an event", async () => {
  const { fetch } = fakeFetch(() =>
    streamOf('event: delta\ndata: {"te', 'xt":"Hel"}\n\nevent: delta\ndata: {"text":"lo"}\n\n', 'event: result\ndata: {"text":"Hello"}\n\n'),
  );
  const seen = [];
  const result = await stepGateway({ base: "/api", fetch }).runStep(request, { onDelta: (t) => seen.push(t) });
  assert.deepEqual(seen, ["Hel", "lo"]);
  assert.equal(result.text, "Hello");
});

test("an error event rejects with its code", async () => {
  const { fetch } = fakeFetch(() => streamOf('event: delta\ndata: {"text":"Hel"}\n\nevent: error\ndata: {"code":"WRITING_HELPER_FAILED","message":"claude stopped"}\n\n'));
  const err = await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "WRITING_HELPER_FAILED");
  assert.equal(err.message, "claude stopped");
});

test("a stream that ends without a result fails the step", async () => {
  const { fetch } = fakeFetch(() => streamOf('event: delta\ndata: {"text":"Hel"}\n\n'));
  await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "WRITING_HELPER_FAILED");
});

test("a refusal before the stream keeps its code and fix link", async () => {
  const { fetch } = fakeFetch(() =>
    jsonResponse(503, { code: "WRITING_HELPER_UNAVAILABLE", message: "Claude is not signed in", fix_url: "/agent-isolation#credential" }),
  );
  const err = await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "WRITING_HELPER_UNAVAILABLE");
  assert.equal(err.message, "Claude is not signed in");
  assert.equal(err.details.fix_url, "/agent-isolation#credential");
});

test("a refusal in the API's error shape is read too", async () => {
  const { fetch } = fakeFetch(() => jsonResponse(400, { code: "INVALID_INPUT", error: "field.text is too long" }));
  const err = await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "INVALID_INPUT");
  assert.equal(err.message, "field.text is too long");
});

test("an unreachable server is a NETWORK error", async () => {
  const gateway = stepGateway({ base: "/api", fetch: async () => { throw new TypeError("failed to fetch"); } });
  await assert.rejects(gateway.runStep(request), "NETWORK");
});

test("aborting the signal rejects with an AbortError and passes the signal to fetch", async () => {
  const controller = new AbortController();
  const { fetch, calls } = fakeFetch(
    (_, init) => new Promise((_, reject) => init.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")))),
  );
  const pending = stepGateway({ base: "/api", fetch }).runStep(request, { signal: controller.signal });
  controller.abort();
  const err = await pending.then(() => null, (e) => e);
  assert.equal(err.name, "AbortError");
  assert.ok(!(err instanceof StructuredError));
  assert.equal(calls[0].init.signal, controller.signal);
});

test("an event whose data is not an object is a bad answer, not a lost connection", async () => {
  const { fetch } = fakeFetch(() => streamOf("event: delta\ndata: null\n\n"));
  const err = await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "WRITING_HELPER_FAILED");
  assert.ok(err.message.includes("cannot be read"));
});

test("an error event may carry the fix link too", async () => {
  const { fetch } = fakeFetch(() => streamOf('event: error\ndata: {"code":"WRITING_HELPER_UNAVAILABLE","message":"not signed in","fix_url":"/agent-isolation#credential"}\n\n'));
  const err = await assert.rejects(stepGateway({ base: "/api", fetch }).runStep(request), "WRITING_HELPER_UNAVAILABLE");
  assert.equal(err.details.fix_url, "/agent-isolation#credential");
});
