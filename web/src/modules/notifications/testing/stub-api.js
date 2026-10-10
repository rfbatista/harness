// A stand-in for the harness server's notification routes, backed by the
// memory gateway: a fetch and an EventSource that speak the Web UI contract's
// wire format. The real notificationsGateway runs the contract suite
// against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryNotifications } from "../infrastructure/memory-gateway.js";
import { notificationDTO } from "./fixtures.js";

const STATUS = { NOTIFICATION_NOT_FOUND: 404, INVALID_INPUT: 400 };

export function stubNotificationsApi(world) {
  const memory = memoryNotifications(world);
  const { gateway } = memory;
  const sources = new Set();

  const fail = (err) => jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/notifications") {
        const q = url.searchParams;
        const limit = q.has("limit") ? Number(q.get("limit")) : undefined;
        const unread = q.get("unread");
        if (unread !== null && unread !== "true" && unread !== "false") return jsonResponse(400, { error: "unread is true or false", code: "INVALID_INPUT" });
        const page = await gateway.list({ projectId: q.get("project_id") ?? "", unread: unread === "true", before: q.get("before") ?? "", limit });
        return jsonResponse(200, {
          notifications: page.notifications.map(notificationDTO),
          unread_count: page.unreadCount,
          next_before: page.nextBefore || undefined,
        });
      }
      if (method === "POST" && url.pathname === "/api/notifications/read") {
        const request = body.ids ? { ids: body.ids } : body.all === true ? { all: true, projectId: body.project_id } : {};
        const change = await gateway.markRead(request);
        return jsonResponse(200, { notifications: change.notifications.map(notificationDTO), unread_count: change.unreadCount });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      if (url.pathname !== "/api/notifications/events") throw new Error(`no stream at ${url.pathname}`);
      sources.add(this);
      this.unfollow = gateway.follow(
        (change) => this.send({ changed: change.notifications.map(notificationDTO), unread_count: change.unreadCount }),
        () => {},
      );
      queueMicrotask(() => this.onopen?.({}));
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      sources.delete(this);
      this.unfollow();
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}
