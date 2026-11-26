import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, ApiError, type FetchLike } from "../../../shared/infrastructure/api-client";
import { wireKill, wireRead } from "../../../shared/testing/memory-fixtures";
import { httpMemorySource } from "./http-memory-source";

const reply = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const sourceAnswering = (answer: Response) => {
  const urls: string[] = [];
  const fetcher: FetchLike = async (url) => {
    urls.push(url);
    return answer;
  };
  return { urls, source: httpMemorySource(apiClient(fetcher)) };
};

describe("HTTP memory source", () => {
  test("reads the session's memory at the contract's path, with its kills, source and prevention", async () => {
    const { urls, source } = sourceAnswering(reply(200, wireRead({ kills: [wireKill()], budget_source: "global" })));
    const read = await source.read("a/b");
    assert.deepEqual(urls, ["/api/sessions/a%2Fb/memory"]);
    assert.equal(read?.kills[0]?.command, "node jest");
    assert.deepEqual([read?.budgetSource, read?.prevention], ["global", { heapMb: 1365, testWorkers: 2 }]);
  });

  test("a session that is not live (404) reads as null", async () => {
    const { source } = sourceAnswering(reply(404, { error: "session is not live", code: "SESSION_NOT_FOUND" }));
    assert.equal(await source.read("s1"), null);
  });

  test("any other failure rejects", async () => {
    const { source } = sourceAnswering(reply(500, { error: "boom", code: "INTERNAL" }));
    await assert.rejects(source.read("s1"), ApiError);
  });
});
