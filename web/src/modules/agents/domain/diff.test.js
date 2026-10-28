import { assert, file, test } from "../../../shared/testing/test.js";
import { conflictModel, diffModel } from "./diff.js";

file("agents/domain/diff");

const kinds = (f) => f.lines.map((l) => l.mark + l.text).join("\n");

test("a changed file shows only the lines that differ, with a little context around them", () => {
  const before = ["{", '  "a": 1,', '  "b": 2,', '  "c": 3,', '  "d": 4,', '  "e": 5,', '  "f": 6,', '  "g": 7', "}"].join("\n");
  const after = before.replace('"b": 2', '"b": 20');
  const [f] = diffModel([{ path: "agents.json", before, after }]);
  assert.equal(f.status, "changed");
  assert.deepEqual([f.added, f.removed], [1, 1]);
  assert.deepEqual(f.lines.map((l) => [l.mark, l.text]), [
    [" ", "{"], [" ", '  "a": 1,'], ["−", '  "b": 2,'], ["+", '  "b": 20,'],
    [" ", '  "c": 3,'], [" ", '  "d": 4,'], [" ", '  "e": 5,'], ["", "…"],
  ]);
});

test("a file that did not exist before is all additions, and one that is gone is all removals", () => {
  const [made, gone] = diffModel([
    { path: "skills/x/SKILL.md", before: null, after: "one\ntwo" },
    { path: "skills/y/SKILL.md", before: "a\nb\nc", after: null },
  ]);
  assert.equal(made.status, "new");
  assert.equal(made.label, "new file");
  assert.deepEqual(made.lines.map((l) => l.mark), ["+", "+"]);
  assert.equal(gone.status, "deleted");
  assert.equal(gone.label, "deleted");
  assert.deepEqual([gone.added, gone.removed], [0, 3]);
});

test("an empty new file and an unchanged one are said so, not drawn", () => {
  const [empty, same] = diffModel([
    { path: "a", before: null, after: "" },
    { path: "b", before: "x\n", after: "x\n" },
  ]);
  assert.equal(empty.status, "new");
  assert.equal(empty.lines.length, 0);
  assert.equal(same.status, "same");
  assert.equal(same.label, "no change");
});

test("a binary file is named, never printed", () => {
  const [f] = diffModel([{ path: "skills/x/logo.png", before: "AAAA", after: "BBBB", encoding: "base64" }]);
  assert.equal(f.status, "binary");
  assert.equal(f.lines.length, 0);
});

test("the diff of a rename shows where every reference moved", () => {
  const before = ['"bundles": {', '  "ux": ["lint"]', "},", '"agents": {', '  "dev": ["@ux"]', "}"].join("\n");
  const after = before.replace(/ux/g, "design");
  const [f] = diffModel([{ path: "agents.json", before, after }]);
  assert.deepEqual([f.added, f.removed], [2, 2]);
});

test("a conflict shows what changed on disk since the version the editor read", () => {
  const [f, gone] = conflictModel(
    [{ path: "agents.json", theirs: "a\nb2\nc" }, { path: "skills/x", theirs: null }],
    { "agents.json": "a\nb\nc" },
  );
  assert.equal(f.status, "changed");
  assert.equal(kinds(f), [" a", "−b", "+b2", " c"].join("\n"));
  assert.equal(gone.status, "deleted");
  assert.equal(gone.label, "deleted on disk");
});

test("a conflict on a file the editor never saw shows the file as it is now", () => {
  const [f] = conflictModel([{ path: "agents.json", theirs: "x\ny" }], {});
  assert.equal(f.status, "current");
  assert.equal(kinds(f), " x\n y");
});
