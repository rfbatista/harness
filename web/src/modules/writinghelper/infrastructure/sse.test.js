import { assert, file, test } from "../../../shared/testing/test.js";
import { sseParser } from "./sse.js";

file("writinghelper/infrastructure/sse");

function collect() {
  const events = [];
  return { events, parser: sseParser((name, data) => events.push([name, data])) };
}

test("an event is its name and data line", () => {
  const { events, parser } = collect();
  parser.push('event: delta\ndata: {"text":"Hi"}\n\n');
  assert.deepEqual(events, [["delta", '{"text":"Hi"}']]);
});

test("a chunk may split an event anywhere", () => {
  const { events, parser } = collect();
  const text = 'event: delta\ndata: {"text":"Hi"}\n\nevent: result\ndata: {"text":"Hi!"}\n\n';
  for (const ch of text) parser.push(ch);
  assert.deepEqual(events, [["delta", '{"text":"Hi"}'], ["result", '{"text":"Hi!"}']]);
});

test("CRLF line ends work", () => {
  const { events, parser } = collect();
  parser.push("event: result\r\ndata: {}\r\n\r\n");
  assert.deepEqual(events, [["result", "{}"]]);
});

test("several data lines join with a newline; comments and unknown fields are ignored", () => {
  const { events, parser } = collect();
  parser.push(": ping\nid: 4\nevent: delta\ndata: a\ndata: b\n\n");
  assert.deepEqual(events, [["delta", "a\nb"]]);
});

test("an event without a name is a message", () => {
  const { events, parser } = collect();
  parser.push("data: x\n\n");
  assert.deepEqual(events, [["message", "x"]]);
});

test("an event cut off at the end of the stream is dropped", () => {
  const { events, parser } = collect();
  parser.push("event: delta\ndata: {");
  parser.end();
  assert.deepEqual(events, []);
});

test("an unterminated final event is delivered when its data is complete", () => {
  const { events, parser } = collect();
  parser.push('event: result\ndata: {"text":"x"}\n');
  parser.end();
  assert.deepEqual(events, [["result", '{"text":"x"}']]);
});
