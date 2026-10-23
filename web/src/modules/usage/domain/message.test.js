import { assert, file, test } from "../../../shared/testing/test.js";
import { streamEvents } from "../testing/liveCalls.js";
import { reassembleMessage } from "./message.js";

file("usage/domain/message");

test("a stream with no events gives no message", () => {
  assert.equal(reassembleMessage(null), null);
  assert.equal(reassembleMessage([]), null);
});

test("blocks come back in order: thinking, text, tool use with its input", () => {
  const m = reassembleMessage(streamEvents());
  assert.equal(m.id, "msg_1");
  assert.equal(m.model, "claude-sonnet");
  assert.equal(m.role, "assistant");
  assert.deepEqual(m.content, [
    { type: "thinking", thinking: "Let me think." },
    { type: "text", text: "Hello" },
    { type: "tool_use", id: "toolu_1", name: "read_file", input: { path: "a.go" } },
  ]);
  assert.equal(m.stop_reason, "tool_use");
});

test("usage is the start usage updated by the final one", () => {
  assert.deepEqual(reassembleMessage(streamEvents()).usage, { input_tokens: 10, output_tokens: 42 });
});

test("a tool use whose input JSON is cut off keeps the partial text", () => {
  const cut = streamEvents().filter((e) => !(e.event === "content_block_delta" && e.data.delta.partial_json === '"a.go"}'));
  const tool = reassembleMessage(cut).content[2];
  assert.equal(tool.input, null);
  assert.equal(tool.partial_json, '{"path":');
});

test("a tool use with no input deltas has an empty input", () => {
  const evs = streamEvents().filter((e) => !(e.event === "content_block_delta" && e.data.delta.type === "input_json_delta"));
  assert.deepEqual(reassembleMessage(evs).content[2].input, {});
});

test("events whose data did not parse, and unknown events, are skipped", () => {
  const evs = [{ event: "weird", data: "not json" }, ...streamEvents(), { event: "future", data: { type: "future" } }];
  assert.equal(reassembleMessage(evs).content.length, 3);
});

test("a stream cut off before message_start still gives the blocks it has", () => {
  const evs = streamEvents().slice(1, 5);
  const m = reassembleMessage(evs);
  assert.deepEqual(m.content, [{ type: "thinking", thinking: "Let me think." }]);
  assert.equal(m.usage, null);
});
