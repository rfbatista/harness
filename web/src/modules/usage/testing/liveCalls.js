// Fixtures shaped like the live call stream contract (harness server <-> web UI).

/** The data of a `call.started` event. */
export function started(over = {}) {
  return {
    id: "c1",
    attribution: "session:s1",
    project_id: "p1",
    session: { id: "s1", title: "Port the parser" },
    task: { id: "t1", title: "Parser" },
    kind: "session",
    started_at: "2026-01-02T03:04:05.000Z",
    request: {
      method: "POST",
      path: "/v1/messages",
      query: "",
      headers: { "x-api-key": ["[redacted]"], "content-type": ["application/json"] },
      body: { model: "claude-sonnet", messages: [{ role: "user", content: "Hello there" }] },
    },
    ...over,
  };
}

/** The data of a `call.finished` event. */
export function finished(over = {}) {
  return {
    id: "c1",
    ended_at: "2026-01-02T03:04:06.234Z",
    duration_ms: 1234,
    status: 200,
    streamed: false,
    model: "claude-sonnet",
    error: "",
    usage: { input_tokens: 10, output_tokens: 5, cache_creation_input_tokens: 0, cache_read_input_tokens: 0, estimated_cost_usd: 0.01 },
    response: { headers: { "content-type": ["application/json"] }, body: { content: [{ type: "text", text: "Hi" }] }, events: null },
    ...over,
  };
}

/** The data of a `calls.missed` event. */
export const missed = (count = 3, at = "2026-01-02T03:04:07.000Z") => ({ count, at });

const ev = (event, data) => ({ event, data });

/** The SSE events of a streamed response: a thinking block, text, and a tool use with partial JSON input. */
export function streamEvents() {
  return [
    ev("message_start", { type: "message_start", message: { id: "msg_1", model: "claude-sonnet", role: "assistant", content: [], usage: { input_tokens: 10, output_tokens: 1 } } }),
    ev("content_block_start", { type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } }),
    ev("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "Let me " } }),
    ev("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "think." } }),
    ev("content_block_stop", { type: "content_block_stop", index: 0 }),
    ev("content_block_start", { type: "content_block_start", index: 1, content_block: { type: "text", text: "" } }),
    ev("ping", { type: "ping" }),
    ev("content_block_delta", { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "Hel" } }),
    ev("content_block_delta", { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "lo" } }),
    ev("content_block_stop", { type: "content_block_stop", index: 1 }),
    ev("content_block_start", { type: "content_block_start", index: 2, content_block: { type: "tool_use", id: "toolu_1", name: "read_file", input: {} } }),
    ev("content_block_delta", { type: "content_block_delta", index: 2, delta: { type: "input_json_delta", partial_json: '{"path":' } }),
    ev("content_block_delta", { type: "content_block_delta", index: 2, delta: { type: "input_json_delta", partial_json: '"a.go"}' } }),
    ev("content_block_stop", { type: "content_block_stop", index: 2 }),
    ev("message_delta", { type: "message_delta", delta: { stop_reason: "tool_use" }, usage: { output_tokens: 42 } }),
    ev("message_stop", { type: "message_stop" }),
  ];
}
