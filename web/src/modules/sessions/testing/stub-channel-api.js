// A stand-in for the harness server's architect-channel routes, backed by the
// memory channel: a fetch and an EventSource that speak the Web UI contract's
// wire format. The real channelGateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryChannel } from "../infrastructure/memory-channel.js";
import { messageDTO } from "./channel-fixtures.js";
import { statusCheckDTO } from "./fixtures.js";

const STATUS = { STATUS_CHECK_NOT_FOUND: 404, INVALID_INPUT: 400 };

export function stubChannelApi(world) {
  const memory = memoryChannel(world);
  const { gateway } = memory;
  const sources = new Set();

  const fail = (err) => jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/task_messages") {
        const since = url.searchParams.get("since");
        const list = await gateway.listMessages({
          ticketId: url.searchParams.get("ticket_id") ?? "",
          sessionId: url.searchParams.get("session_id") ?? undefined,
          since: since ? new Date(since) : null,
        });
        return jsonResponse(200, { messages: list.map(messageDTO) });
      }
      if (method === "POST" && url.pathname === "/api/set_status_check") {
        const check = await gateway.setStatusCheck(body.delegate_session_id, body.every_minutes);
        return jsonResponse(200, { status_check: statusCheckDTO(check) });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  // The project feed carries every kind of change; the gateway must pick out
  // the task messages and ignore the rest.
  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(
        url.searchParams.get("project_id") ?? "",
        (event) => this.send({ task_message: messageDTO(event.message) }),
        () => {},
      );
      queueMicrotask(() => {
        this.onopen?.({});
        this.send({ ticket: { id: "t1", project_id: "p1", title: "Other news", status: "todo" } });
      });
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
    /** Sends any value on every open stream, as the server would. */
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}
