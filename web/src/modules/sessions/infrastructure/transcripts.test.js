// Both TranscriptGateway implementations run the same contract; the DTO's
// own edges are tested here.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { isTranscriptMissing } from "../domain/transcript.js";
import { stubTranscriptApi } from "../testing/stub-transcript-api.js";
import { transcriptGatewayContract } from "../testing/transcript-contract.js";
import { memoryTranscripts } from "./memory-transcripts.js";
import { toTranscriptPage } from "./transcript-dto.js";
import { transcriptGateway } from "./transcript-gateway.js";

file("sessions/infrastructure/transcripts");

transcriptGatewayContract("memory", (world) => {
  const memory = memoryTranscripts(world);
  return { gateway: memory.gateway, append: memory.append };
});

transcriptGatewayContract("http", (world) => {
  const stub = stubTranscriptApi(world);
  return { gateway: transcriptGateway(apiClient({ base: "/api", fetch: stub.fetch })), append: stub.memory.append };
});

test("the DTO skips entry kinds it does not know and rejects a page without entries", () => {
  const page = toTranscriptPage({
    entries: [
      { cursor: "a", kind: "summary", text: "x" },
      { cursor: "b", kind: "assistant", text: "<b>hi</b>", at: "" },
    ],
    first_cursor: "a",
    last_cursor: "b",
    has_older: false,
    has_newer: false,
  });
  assert.deepEqual(page.entries.map((e) => [e.cursor, e.text, e.at]), [["b", "<b>hi</b>", null]]);
  assert.throws(() => toTranscriptPage({}), Codes.BAD_RESPONSE);
  assert.throws(() => toTranscriptPage({ entries: [{ kind: "user" }] }), Codes.BAD_RESPONSE);
  assert.throws(() => toTranscriptPage({ entries: [{ cursor: "a", kind: "user", at: "yesterday" }] }), Codes.BAD_RESPONSE);
});

test("the gateway asks for the session's transcript with its cursor and limit", async () => {
  const urls = [];
  const { fetch } = fakeFetch((url) => {
    urls.push(url);
    return jsonResponse(200, { entries: [], first_cursor: "", last_cursor: "", has_older: false, has_newer: false });
  });
  const gateway = transcriptGateway(apiClient({ base: "/api", fetch }));
  await gateway.page("a b", { before: "c1", limit: 50 });
  await gateway.page("s2");
  assert.deepEqual(urls, ["/api/sessions/a%20b/transcript?before=c1&limit=50", "/api/sessions/s2/transcript"]);
});

test("missing: no saved conversation, an unknown session, or a server without the route", async () => {
  const gateway = (status, body) => transcriptGateway(apiClient({ base: "/api", fetch: fakeFetch(() => new Response(body, { status })).fetch }));
  const caught = async (g) => g.page("s1").catch((err) => err);
  assert.ok(isTranscriptMissing(await caught(gateway(404, JSON.stringify({ code: "SESSION_TRANSCRIPT_MISSING", error: "none" })))));
  assert.ok(isTranscriptMissing(await caught(gateway(404, JSON.stringify({ code: "SESSION_NOT_FOUND", error: "gone" })))));
  assert.ok(isTranscriptMissing(await caught(gateway(404, "404 page not found"))));
  assert.ok(!isTranscriptMissing(await caught(gateway(500, JSON.stringify({ code: "UNKNOWN", error: "boom" })))));
  assert.ok(!isTranscriptMissing(await caught(gateway(400, JSON.stringify({ code: "INVALID_INPUT", error: "bad cursor" })))));
});
