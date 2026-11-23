// A fake harness server for the HTTP adapter's tests: it answers the notifications
// routes on the wire, from an in-memory source that holds the data.

import type { FetchLike } from "../../../shared/infrastructure/api-client";
import { NotificationsSourceError } from "../application/ports";
import type { MemoryNotificationsSource } from "../infrastructure/memory-notifications-source";
import { pageToWire, toWire } from "./wire";

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const STATUS: Record<string, number> = { INVALID_INPUT: 400, NOTIFICATION_NOT_FOUND: 404 };

export interface FakeServer {
  fetch: FetchLike;
  /** Every next request fails as the network does. */
  breakNetwork(): void;
  /** Every next request is answered 200 with `raw`, whatever it asked. */
  answerWith(raw: unknown): void;
  readonly requests: readonly { method: string; url: string; body: unknown }[];
}

export function fakeNotificationsServer(data: MemoryNotificationsSource): FakeServer {
  let down = false;
  let fixed: { raw: unknown } | null = null;
  const requests: { method: string; url: string; body: unknown }[] = [];
  const failed = (err: unknown) => {
    if (err instanceof NotificationsSourceError) return json(STATUS[err.code] ?? 500, { error: err.message, code: err.code });
    throw err;
  };
  return {
    requests,
    breakNetwork: () => void (down = true),
    answerWith: (raw) => void (fixed = { raw }),
    fetch: async (url, init) => {
      if (init?.signal?.aborted) throw new DOMException("aborted", "AbortError");
      if (down) throw new TypeError("offline");
      if (fixed) return json(200, fixed.raw);
      const method = init?.method ?? "GET";
      requests.push({ method, url, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined });
      const target = new URL(url, "http://harness.test");
      try {
        if (method === "GET" && target.pathname === "/api/notifications") {
          const limit = target.searchParams.get("limit");
          const page = await data.list({ unread: target.searchParams.get("unread") === "true", before: target.searchParams.get("before") ?? undefined, ...(limit === null ? {} : { limit: Number(limit) }) });
          return json(200, pageToWire(page));
        }
        if (method === "POST" && target.pathname === "/api/notifications/read") {
          const body = (typeof init?.body === "string" ? JSON.parse(init.body) : {}) as { ids?: string[]; all?: true };
          const answer = await data.markRead({ ...(body.ids ? { ids: body.ids } : {}), ...(body.all ? { all: true } : {}) } as never);
          return json(200, { notifications: answer.notifications.map(toWire), unread_count: answer.unreadCount });
        }
      } catch (err) {
        return failed(err);
      }
      return json(404, { error: "no such route", code: "NOT_FOUND" });
    },
  };
}
