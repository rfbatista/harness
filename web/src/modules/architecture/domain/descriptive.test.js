import { assert, file, test } from "../../../shared/testing/test.js";
import { kindLabel, loadedLabel, noUsage, reaches, usageText } from "./descriptive.js";

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
