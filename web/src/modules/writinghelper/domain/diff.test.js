import { assert, file, test } from "../../../shared/testing/test.js";
import { diffHtml, wordDiff } from "./diff.js";

file("writinghelper/domain/diff");

const kinds = (segments) => segments.map((s) => `${s.type}:${s.text}`);

test("identical text is one unchanged segment", () => {
  assert.deepEqual(kinds(wordDiff("same words", "same words")), ["eq:same words"]);
});

test("an added word is an insertion", () => {
  assert.deepEqual(kinds(wordDiff("the pen", "the magic pen")), ["eq:the ", "ins:magic ", "eq:pen"]);
});

test("a removed word is a removal", () => {
  assert.deepEqual(kinds(wordDiff("the magic pen", "the pen")), ["eq:the ", "del:magic ", "eq:pen"]);
});

test("a replaced word is a removal then an insertion", () => {
  assert.deepEqual(kinds(wordDiff("it should read", "it reads")), ["eq:it ", "del:should read", "ins:reads"]);
});

test("an empty field makes the whole draft an insertion", () => {
  assert.deepEqual(kinds(wordDiff("", "Hello there")), ["ins:Hello there"]);
});

test("an empty draft makes the whole field a removal", () => {
  assert.deepEqual(kinds(wordDiff("Hello", "")), ["del:Hello"]);
});

test("the segments rebuild both texts", () => {
  const before = "Some places like descriptions and titles could have a magic pen.";
  const after = "Add a magic pen to titles and descriptions.\n\nDone when: [open: what done looks like]";
  const segments = wordDiff(before, after);
  assert.equal(segments.filter((s) => s.type !== "ins").map((s) => s.text).join(""), before);
  assert.equal(segments.filter((s) => s.type !== "del").map((s) => s.text).join(""), after);
});

test("a very large pair falls back to a whole replacement", () => {
  const a = Array.from({ length: 3000 }, (_, i) => `a${i}`).join(" ");
  const b = Array.from({ length: 3000 }, (_, i) => `b${i}`).join(" ");
  assert.deepEqual(wordDiff(a, b).map((s) => s.type), ["del", "ins"]);
});

test("an open placeholder stays one unit and is marked as a gap", () => {
  const segments = wordDiff("Done when: ", "Done when: [open: what done looks like]");
  assert.equal(segments.at(-1).type, "ins");
  assert.equal(segments.at(-1).text, "[open: what done looks like]");
  assert.equal(segments.at(-1).gap, true);
});

test("diffHtml marks insertions, removals and gaps, and escapes the text", () => {
  const html = diffHtml(wordDiff("a <b> c", "a <i> c [open: why]"));
  assert.equal(html, 'a <del>&lt;b&gt;</del><ins>&lt;i&gt;</ins> c<ins> <span class="gap">[open: why]</span></ins>');
});

test("diffHtml marks a gap that was already in the text too", () => {
  assert.equal(diffHtml([{ type: "eq", text: "x [open: y]" }]), 'x <span class="gap">[open: y]</span>');
});
