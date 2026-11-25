// A fake harness server for the HTTP adapter's tests: it answers the product owner's
// routes on the wire, from an in-memory product owner that holds the data.

import type { FetchLike } from "../../../shared/infrastructure/api-client";
import { ProductOwnerError } from "../application/ports";
import type { MemoryProductOwner } from "../infrastructure/memory-product-owner";
import { digestPageWire, headWire, itemWire, settingsWire, summaryWire } from "./wire";

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const STATUS: Record<string, number> = { PRODUCT_OWNER_OFF: 409, THREAD_BUSY: 409, INVALID_INPUT: 400, PRODUCT_OWNER_UNAVAILABLE: 503, PROJECT_NOT_FOUND: 404, THREAD_NOT_FOUND: 404 };

export interface FakeServer {
  fetch: FetchLike;
  breakNetwork(): void;
  /** Every next request is answered 200 with `raw`, whatever it asked. */
  answerWith(raw: unknown): void;
  readonly requests: readonly { method: string; url: string; body: unknown }[];
}

export function fakeProductOwnerServer(data: MemoryProductOwner): FakeServer {
  let down = false;
  let fixed: { raw: unknown } | null = null;
  const requests: { method: string; url: string; body: unknown }[] = [];
  const failed = (err: unknown) => {
    if (err instanceof ProductOwnerError) return json(STATUS[err.code] ?? 500, { error: err.message, code: err.code, ...(err.fixUrl === undefined ? {} : { fix_url: err.fixUrl }) });
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
      const body = (typeof init?.body === "string" ? JSON.parse(init.body) : {}) as Record<string, string | boolean>;
      requests.push({ method, url, body: typeof init?.body === "string" ? body : undefined });
      const target = new URL(url, "http://harness.test");
      const q = (key: string): string => target.searchParams.get(key) ?? "";
      try {
        const route = `${method} ${target.pathname}`;
        switch (route) {
          case "GET /api/product_owner":
            return json(200, settingsWire(await data.settings(q("project_id"))));
          case "POST /api/update_product_owner":
          {
            // Only the fields sent change.
            const projectId = String(body.project_id);
            if (body.enabled !== undefined) await data.setEnabled(projectId, body.enabled === true);
            if (body.digest_time !== undefined) await data.updateSettings(projectId, { digestTime: String(body.digest_time) });
            return json(200, settingsWire(await data.settings(projectId)));
          }
          case "GET /api/product_owner_digests":
            return json(200, digestPageWire(await data.digests(q("project_id"), target.searchParams.get("before") ?? undefined)));
          case "GET /api/product_owner_threads":
            return json(200, { threads: (await data.threads(q("project_id"))).map(summaryWire) });
          case "GET /api/product_owner_thread": {
            const page = await data.thread(q("thread_id"), target.searchParams.get("before") ?? undefined);
            return json(200, { thread: headWire(page.head), items: page.items.map(itemWire), has_earlier: page.hasEarlier });
          }
          case "POST /api/start_product_owner_thread": {
            const started = await data.startThread(String(body.project_id), String(body.message));
            return json(200, { thread_id: started.threadId, item_id: started.itemId });
          }
          case "POST /api/send_product_owner_message":
            return json(200, { item_id: (await data.sendMessage(String(body.thread_id), String(body.message))).itemId });
          case "POST /api/stop_product_owner_turn":
            await data.stopTurn(String(body.thread_id));
            return json(200, {});
          case "POST /api/rename_product_owner_thread":
            return json(200, summaryWire(await data.renameThread(String(body.thread_id), String(body.title))));
        }
      } catch (err) {
        return failed(err);
      }
      return json(404, { error: "no such route", code: "NOT_FOUND" });
    },
  };
}
