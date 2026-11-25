// How the HTTP notifications adapter fails when the server does not answer as
// the contract says: every failure is a NotificationsSourceError, never a raw one.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { NotificationsSourceError } from "../application/ports";
import { httpNotificationsSource } from "./http-notifications-source";

const sourceAnswering = (respond: () => Response) => httpNotificationsSource(apiClient((async () => respond()) as FetchLike), memoryFeed());
const failsWith = (code: string) => (err: unknown) => err instanceof NotificationsSourceError && err.code === code;

describe("HTTP notifications adapter: answers off the contract", () => {
  test("a page that is not a page is BAD_RESPONSE", async () => {
    await assert.rejects(sourceAnswering(() => Response.json({ nope: true })).list({}), failsWith("BAD_RESPONSE"));
  });

  test("an unread_count that is negative or not a number is BAD_RESPONSE", async () => {
    for (const unread_count of [-1, "3", 1.5, null]) await assert.rejects(sourceAnswering(() => Response.json({ notifications: [], unread_count })).list({}), failsWith("BAD_RESPONSE"));
  });

  test("a notification whose link leaves the web UI is BAD_RESPONSE", async () => {
    const n = { id: "n1", source: "system", kind: "task_review", level: "info", link: "//evil.example/x", created_at: "2026-11-21T12:00:00Z" };
    await assert.rejects(sourceAnswering(() => Response.json({ notifications: [n], unread_count: 1 })).list({}), failsWith("BAD_RESPONSE"));
  });

  test("a 500 on list is UNKNOWN", async () => {
    await assert.rejects(sourceAnswering(() => new Response("oops", { status: 500 })).list({}), failsWith("UNKNOWN"));
  });

  test("a 500 on mark all read is UNKNOWN", async () => {
    await assert.rejects(sourceAnswering(() => new Response("oops", { status: 500 })).markRead({ all: true }), failsWith("UNKNOWN"));
  });

  test("a 200 with an HTML body on list is a NotificationsSourceError, not a raw SyntaxError", async () => {
    await assert.rejects(sourceAnswering(() => new Response("<html>sign in</html>", { status: 200 })).list({}), failsWith("BAD_RESPONSE"));
  });

  test("a 200 with an HTML body on mark all read is a NotificationsSourceError, not a raw SyntaxError", async () => {
    await assert.rejects(sourceAnswering(() => new Response("<html>sign in</html>", { status: 200 })).markRead({ all: true }), failsWith("BAD_RESPONSE"));
  });
});
