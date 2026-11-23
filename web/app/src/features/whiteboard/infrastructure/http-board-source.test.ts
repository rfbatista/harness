import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { BoardSourceError } from "../application/ports";
import { httpBoardSource } from "./http-board-source";

const answering = (respond: () => Response) => httpBoardSource(apiClient((async () => respond()) as FetchLike));
const failsWith = (code: string) => (err: unknown) => err instanceof BoardSourceError && err.code === code;

const wire = { id: "s1", status: "running", role: "delegate", parent_session_id: "s0", brought_under: true, agent_id: "a1", created_at: "2026-11-23T12:00:00Z", updated_at: "2026-11-23T12:01:00Z", unknown_extra: 1 };

describe("HTTP board source: sessions", () => {
  test("reads the sessions of the task and asks for that task only", async () => {
    const urls: string[] = [];
    const source = httpBoardSource(apiClient((async (url: string) => (urls.push(url), Response.json({ sessions: [wire] }))) as FetchLike));
    const [s] = await source.sessions("t1");
    assert.deepEqual(urls, ["/api/sessions?ticket_id=t1"]);
    assert.equal(s?.id, "s1");
    assert.equal(s?.parentSessionId, "s0");
    assert.equal(s?.broughtUnder, true);
    assert.equal(s?.createdAt.toISOString(), "2026-11-23T12:00:00.000Z");
  });

  test("an answer that is not a session list is BAD_RESPONSE", async () => {
    await assert.rejects(answering(() => Response.json({ nope: 1 })).sessions("t1"), failsWith("BAD_RESPONSE"));
  });

  test("a session with an unknown status or no created_at is BAD_RESPONSE", async () => {
    await assert.rejects(answering(() => Response.json({ sessions: [{ ...wire, status: "bogus" }] })).sessions("t1"), failsWith("BAD_RESPONSE"));
    await assert.rejects(answering(() => Response.json({ sessions: [{ ...wire, created_at: undefined }] })).sessions("t1"), failsWith("BAD_RESPONSE"));
  });

  test("a failing server is UNKNOWN, a missing network is NETWORK_ERROR", async () => {
    await assert.rejects(answering(() => new Response("oops", { status: 500 })).sessions("t1"), failsWith("UNKNOWN"));
    const down = httpBoardSource(apiClient((async () => { throw new TypeError("offline"); }) as FetchLike));
    await assert.rejects(down.sessions("t1"), failsWith("NETWORK_ERROR"));
  });
});
