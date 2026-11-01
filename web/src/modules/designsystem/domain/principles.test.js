import { assert, file, test } from "../../../shared/testing/test.js";
import { outlineOf } from "./principles.js";

file("designsystem/domain/principles");

test("the outline lists the ## sections in order, each with the line and the offset it starts at", () => {
  const md = "# Calm\nIntro\n\n## Colour\nUse signal.\n\n## Space\nBreathe.\n";
  assert.deepEqual(outlineOf(md), [
    { key: "0:Colour", title: "Colour", line: 3, offset: md.indexOf("## Colour") },
    { key: "1:Space", title: "Space", line: 6, offset: md.indexOf("## Space") },
  ]);
});

test("only ## headings are sections: # and ### are not, nor is a hash without a space", () => {
  assert.deepEqual(outlineOf("# One\n### Three\n##Tight\n#### Four\n").length, 0);
});

test("a ## line inside a fenced code block is not a section", () => {
  const md = "## Real\n```md\n## Not a section\n```\n## Also real\n";
  assert.deepEqual(outlineOf(md).map((s) => s.title), ["Real", "Also real"]);
});

test("a heading's closing hashes and spaces are not part of its title, and an empty one is skipped", () => {
  assert.deepEqual(outlineOf("## Motion ##  \n## \n").map((s) => s.title), ["Motion"]);
});

test("no text, no outline", () => {
  assert.deepEqual(outlineOf(""), []);
  assert.deepEqual(outlineOf(undefined), []);
});

test("windows line endings count the same lines", () => {
  const md = "## A\r\ntext\r\n## B\r\n";
  assert.deepEqual(outlineOf(md).map((s) => [s.title, s.line, s.offset]), [["A", 0, 0], ["B", 2, md.indexOf("## B")]]);
});
