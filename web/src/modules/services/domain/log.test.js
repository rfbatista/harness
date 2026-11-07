import { assert, file, test } from "../../../shared/testing/test.js";
import { MAX_LINES, applyChunk, applyReset, emptyLog, fromRead, runLine, tailNote } from "./log.js";

file("services/domain/log");

const read = (o = {}) => ({ text: "", offset: 0, next_offset: 0, first_offset: 0, run_id: "r1", ...o });
const texts = (log) => log.lines.map((l) => l.text);

test("a read from the start of the run becomes lines; a harness line is marked", () => {
  const log = fromRead(read({ text: "[harness] command: make air\nlistening :8080\n", next_offset: 44 }));
  assert.deepEqual(texts(log), ["[harness] command: make air", "listening :8080"]);
  assert.deepEqual(log.lines.map((l) => l.harness), [true, false]);
  assert.equal(log.end, 44);
  assert.equal(log.runId, "r1");
});

test("a harness line shows its prefix apart from the rest, so the prefix can be bold", () => {
  const [line] = fromRead(read({ text: "[harness] sandboxed: yes\n", next_offset: 25 })).lines;
  assert.deepEqual([line.prefix, line.body], ["[harness]", " sandboxed: yes"]);
  const [plain] = fromRead(read({ text: "hello\n", next_offset: 6 })).lines;
  assert.deepEqual([plain.prefix, plain.body], ["", "hello"]);
});

test("a read that starts inside the log drops its first, probably cut, line and says earlier lines exist", () => {
  const log = fromRead(read({ text: "ll of a line\nnext line\n", offset: 1000, next_offset: 1022 }));
  assert.deepEqual(texts(log), ["next line"]);
  assert.equal(log.earlier, true);
});

test("a read from the start has nothing earlier", () => {
  assert.equal(fromRead(read({ text: "a\n", next_offset: 2 })).earlier, false);
});

test("a chunk at the end of what is held is appended; one that ends mid-line is completed by the next", () => {
  let log = fromRead(read({ text: "one\n", next_offset: 4 }));
  let r = applyChunk(log, { offset: 4, text: "tw", run_id: "r1" });
  assert.equal(r.gap, false);
  assert.deepEqual(texts(r.log), ["one", "tw"]);
  r = applyChunk(r.log, { offset: 6, text: "o\nthree\n", run_id: "r1" });
  assert.deepEqual(texts(r.log), ["one", "two", "three"]);
  assert.equal(r.log.end, 14);
  assert.equal(r.added, 2);
});

test("a chunk that is not at the end of what is held is a gap", () => {
  const log = fromRead(read({ text: "one\n", next_offset: 4 }));
  const r = applyChunk(log, { offset: 9, text: "x\n", run_id: "r1" });
  assert.equal(r.gap, true);
  assert.deepEqual(texts(r.log), ["one"]);
});

test("offsets count bytes, not characters", () => {
  const log = fromRead(read({ text: "é\n", next_offset: 3 }));
  assert.equal(applyChunk(log, { offset: 3, text: "ok\n", run_id: "r1" }).gap, false);
});

test("a reset clears the log and marks where the new run began", () => {
  const log = fromRead(read({ text: "old\n", next_offset: 4 }));
  const reset = applyReset(log, { run_id: "r2" }, "14:32");
  assert.deepEqual(reset.lines.map((l) => [l.kind, l.text]), [["reset", "New run started 14:32 · the log was cleared"]]);
  assert.equal(reset.end, 0);
  assert.equal(reset.runId, "r2");
  assert.deepEqual(texts(applyChunk(reset, { offset: 0, text: "fresh\n", run_id: "r2" }).log), ["New run started 14:32 · the log was cleared", "fresh"]);
});

test("the oldest lines are dropped past the cap", () => {
  let log = fromRead(read({ text: "a\n", next_offset: 2 }));
  const many = "x\n".repeat(MAX_LINES + 10);
  log = applyChunk(log, { offset: 2, text: many, run_id: "r1" }).log;
  assert.equal(log.lines.length, MAX_LINES);
  assert.equal(log.earlier, true);
});

test("the note over the log says how much is shown", () => {
  assert.equal(tailNote(fromRead(read({ text: "a\n", next_offset: 2 }))), "");
  assert.equal(tailNote(fromRead(read({ text: "x\ny\n", offset: 500, next_offset: 504 }))), "Showing the last part of the log. The log keeps the last 5 MB of this run.");
});

test("the run line says when, by whom and where", () => {
  const run = { started_at: "2026-11-07T14:02:00", started_by: { kind: "person", agent: "", session_id: "" }, where: { kind: "main", branch: "main" } };
  assert.equal(runLine(run), "Run started 14:02 · by you · main checkout @ main");
  assert.equal(runLine({ ...run, started_by: { kind: "session", agent: "backend", session_id: "s" }, where: { kind: "container" } }), "Run started 14:02 · by backend · container");
  assert.equal(runLine(null), "");
});

test("an empty log has no lines", () => {
  assert.deepEqual(emptyLog().lines, []);
});
