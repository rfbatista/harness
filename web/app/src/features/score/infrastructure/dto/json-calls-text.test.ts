import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { jsonCallsText } from "./json-calls-text";

const parseExpectedCalls = jsonCallsText.parse;
const formatExpectedCalls = jsonCallsText.format;

describe("the expected calls a person writes, one per line", () => {
  test("a tool alone accepts any arguments; a tool with JSON names each parameter", () => {
    const { calls, errors } = parseExpectedCalls('read_file {"path": "README.md"}\n\nsay_done');
    assert.deepEqual(errors, []);
    assert.deepEqual(calls, [{ tool: "read_file", params: { path: "README.md" } }, { tool: "say_done", params: null }]);
  });

  test("a line whose parameters are not a JSON object is refused with its line number", () => {
    const { errors } = parseExpectedCalls('ok\nread_file {"path": }\nlist [1]');
    assert.deepEqual(errors.map((e) => e.line), [2, 3]);
    assert.match(errors[0]?.message ?? "", /JSON object/);
  });

  test("what is formatted parses back to the same calls", () => {
    const calls = [{ tool: "a", params: { x: 1, y: ["z"] } }, { tool: "b", params: null }];
    assert.deepEqual(parseExpectedCalls(formatExpectedCalls(calls)).calls, calls);
  });
});

