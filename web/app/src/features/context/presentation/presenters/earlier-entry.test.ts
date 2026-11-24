import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson, hookJson, serverJson, skillJson, toolJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const first = entryJson({
  id: "e1",
  at: "2026-11-23T20:49:00Z",
  facts: factsJson({ agent: "react-developer" }),
  tools: [toolJson({ name: "Bash" })],
  mcp_servers: [serverJson({ name: "task", attached: "built_in" })],
  skills: [skillJson({ name: "old-skill" })],
  hooks: [hookJson({ rule: "old rule" })],
});
const second = entryJson({
  id: "e2",
  kind: "resume",
  at: "2026-11-24T09:12:00Z",
  facts: factsJson({ agent: "react-developer" }),
  tools: [toolJson({ name: "Bash" }), toolJson({ name: "Read" })],
  mcp_servers: [serverJson({ name: "task" }), serverJson({ name: "github", attached: "agent" })],
  skills: [skillJson({ name: "new-skill" })],
  hooks: [hookJson({ rule: "new rule" }), hookJson({ rule: "another rule" })],
});

const setup = async () => {
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [first, second] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), projectId: "p1", timeZone: "UTC" });
  presenter.open();
  await flush();
  return presenter;
};

describe("an earlier entry of a resumed session", () => {
  test("shows that entry's tools, servers, skills and hooks, not the latest's", async () => {
    const p = await setup();
    p.selectEntry("e1");
    const g = p.viewModel.groups!;
    assert.deepEqual([g.tools.count, g.servers.count, g.skills.count, g.hooks.count], ["1", "1", "1", "1"]);
    assert.deepEqual(g.skills.rows.map((r) => r.name), ["old-skill"]);
    assert.deepEqual(g.hooks.rows.map((r) => r.rule), ["old rule"]);
    assert.deepEqual(g.servers.rows.map((r) => r.name), ["task"]);
  });

  test("the settings links follow the entry on show", async () => {
    const p = await setup();
    assert.ok(p.viewModel.links.some((l) => l.label === "MCP server github in the catalog"));
    p.selectEntry("e1");
    assert.equal(p.viewModel.links.some((l) => l.label === "MCP server github in the catalog"), false);
  });

  test("search keeps its query when the entry changes and filters the entry on show", async () => {
    const p = await setup();
    p.search("rule");
    assert.equal(p.viewModel.groups!.hooks.count, "2 of 2");
    p.selectEntry("e1");
    assert.equal(p.viewModel.search.query, "rule");
    assert.equal(p.viewModel.groups!.hooks.count, "1 of 1");
  });

  test("the picker's labels name the kind of entry and its time in the person's zone", async () => {
    const p = await setup();
    assert.deepEqual(p.viewModel.picker!.options.map((o) => o.label), ["Started 20:49", "Resumed 09:12"]);
  });
});
