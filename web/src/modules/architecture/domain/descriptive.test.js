import { assert, file, test } from "../../../shared/testing/test.js";
import { editedRows, handoffChains, isMissingPath, kindLabel, loadedLabel, noUsage, reaches, usageText } from "./descriptive.js";

file("architecture/domain/descriptive");

test("a row's kind and loading are said in words", () => {
  assert.equal(kindLabel("instruction", "subagent_prompt"), "Sub-agent prompt");
  assert.equal(kindLabel("tool", "mcp"), "MCP server");
  assert.equal(kindLabel("tool", "weird"), "weird");
  assert.equal(loadedLabel("on_demand"), "On demand");
  assert.equal(loadedLabel("always"), "Always");
});

test("a tool that writes with no usage prose is flagged, and its line says what is missing", () => {
  assert.equal(noUsage({ writes: true, usage: "  " }), true);
  assert.equal(noUsage({ writes: true, usage: "Call it last." }), false);
  assert.equal(noUsage({ writes: false }), false);
  assert.equal(usageText({ usage: "Call it last." }), "Call it last.");
  assert.equal(usageText({ writes: true }), "The model gets no guidance on when to call this.");
});

test("a sandbox shows a reach list only when it has items", () => {
  assert.equal(reaches({ can_reach: ["loopback"] }, "can_reach"), true);
  assert.equal(reaches({ can_reach: [] }, "can_reach"), false);
  assert.equal(reaches({}, "cannot_reach"), false);
});

test("a row is edited when it differs from the saved row, whatever empty fields the draft grew", () => {
  const saved = [{ path: "a.md", kind: "rules" }, { path: "b.md", kind: "skill", note: "x" }];
  const same = [{ path: "a.md", kind: "rules", note: "" }, { path: "b.md", kind: "skill", note: "x", applies_to: [] }];
  assert.equal(editedRows(saved, same, (r) => r.path).join(), "false,false");
  const changed = [{ path: "a.md", kind: "rules" }, { path: "b.md", kind: "skill", note: "y" }];
  assert.equal(editedRows(saved, changed, (r) => r.path).join(), "false,true");
});

test("a row added or removed does not shift the edited marks of the others", () => {
  const saved = [{ path: "a.md" }, { path: "b.md" }, { path: "c.md" }];
  assert.equal(editedRows(saved, [{ path: "b.md" }, { path: "c.md" }], (r) => r.path).join(), "false,false"); // a was removed
  assert.equal(editedRows(saved, [...saved, { path: "d.md" }], (r) => r.path).join(), "false,false,false,true"); // d is new
  assert.equal(editedRows([], [{ path: "x" }], (r) => r.path).join(), "true");
  assert.equal(editedRows(undefined, [], (r) => r.path).length, 0);
});

test("a hand-off that continues the last one extends its chain; any other starts a new chain", () => {
  const h = (from, to) => ({ from, to, when: "w" });
  assert.equal(JSON.stringify(handoffChains([h("a", "b"), h("b", "c"), h("c", "d"), h("x", "b")])), '[["a","b","c","d"],["x","b"]]');
  assert.equal(JSON.stringify(handoffChains([h("a", ""), h("", "b")])), "[]"); // a half-filled hand-off draws nothing
  assert.equal(JSON.stringify(handoffChains([])), "[]");
});

test("a path is missing when discovery answered and did not list it, except harness:// paths", () => {
  const found = new Set(["CLAUDE.md"]);
  assert.equal(isMissingPath("GEMINI.md", found), true);
  assert.equal(isMissingPath("CLAUDE.md", found), false);
  assert.equal(isMissingPath("harness://brief", found), false);
  assert.equal(isMissingPath("", found), false);
  assert.equal(isMissingPath("GEMINI.md", null), false); // discovery has not answered
});
