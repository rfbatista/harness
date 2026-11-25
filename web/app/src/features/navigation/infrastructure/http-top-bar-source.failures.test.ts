// How the HTTP top-bar adapter fails when the server does not answer as the
// contract says: every failure is a TopBarSourceError, never a raw one.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { TopBarSourceError } from "../application/ports";
import { topBarJson } from "../testing/fixtures";
import { httpTopBarSource } from "./http-top-bar-source";

const sourceAnswering = (respond: () => Response) => httpTopBarSource(apiClient((async () => respond()) as FetchLike));
const failsWith = (code: string) => (err: unknown) => err instanceof TopBarSourceError && err.code === code;

describe("HTTP top-bar adapter: answers off the contract", () => {
  test("a 200 whose body is not the model is BAD_RESPONSE", async () => {
    await assert.rejects(sourceAnswering(() => Response.json({ hello: "world" })).load("/"), failsWith("BAD_RESPONSE"));
  });

  test("a link of a known key without its href is BAD_RESPONSE, not a bar with a hole in it", async () => {
    const body = topBarJson({ links: [{ key: "board", current: false, attention: false } as never] });
    await assert.rejects(sourceAnswering(() => Response.json(body)).load("/"), failsWith("BAD_RESPONSE"));
  });

  test("a 500 with the API's error body is UNKNOWN with its message", async () => {
    await assert.rejects(sourceAnswering(() => Response.json({ error: "boom", code: "INTERNAL" }, { status: 500 })).load("/"), (err: unknown) => failsWith("UNKNOWN")(err) && (err as Error).message === "boom");
  });

  test("a 502 from a proxy, with an HTML body, is UNKNOWN", async () => {
    await assert.rejects(sourceAnswering(() => new Response("<html>Bad gateway</html>", { status: 502 })).load("/"), failsWith("UNKNOWN"));
  });

  test("a 200 with an HTML body (a login page, a proxy) is a TopBarSourceError, not a raw SyntaxError", async () => {
    await assert.rejects(sourceAnswering(() => new Response("<html>sign in</html>", { status: 200 })).load("/"), failsWith("BAD_RESPONSE"));
  });

  test("a 400 INVALID_INPUT keeps its code", async () => {
    await assert.rejects(sourceAnswering(() => Response.json({ error: "bad path", code: "INVALID_INPUT" }, { status: 400 })).load("x"), failsWith("INVALID_INPUT"));
  });
});
