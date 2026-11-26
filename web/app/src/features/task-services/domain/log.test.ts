import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { applyChunk, fromRead, toneOf } from "./log";

describe("log lines", () => {
  test("[harness] lines and error lines get their tones", () => {
    assert.equal(toneOf("[harness] started"), "harness");
    assert.equal(toneOf("panic: boom"), "error");
    assert.equal(toneOf("listening on :8080"), "plain");
  });

  test("a read shows complete lines and holds a trailing partial line", () => {
    const log = fromRead({ text: "a\nb\nc", offset: 0, nextOffset: 5, runId: "r" });
    assert.deepEqual(log.lines.map((l) => l.text), ["a", "b"]);
    assert.equal(log.partial, "c");
  });

  test("a chunk at the end extends the log; one behind is ignored; one ahead is a gap", () => {
    const log = fromRead({ text: "a\n", offset: 0, nextOffset: 2, runId: "r" });
    const next = applyChunk(log, { kind: "chunk", runId: "r", offset: 2, text: "b\n" });
    assert.deepEqual(next.state.lines.map((l) => l.text), ["a", "b"]);
    assert.equal(applyChunk(next.state, { kind: "chunk", runId: "r", offset: 0, text: "a\n" }).gap, false);
    assert.equal(applyChunk(next.state, { kind: "chunk", runId: "r", offset: 0, text: "a\n" }).state, next.state);
    assert.equal(applyChunk(next.state, { kind: "chunk", runId: "r", offset: 99, text: "x" }).gap, true);
  });

  test("offsets count bytes, not characters", () => {
    const log = fromRead({ text: "é\n", offset: 0, nextOffset: 3, runId: "r" });
    assert.equal(applyChunk(log, { kind: "chunk", runId: "r", offset: 3, text: "b\n" }).gap, false);
  });
});
