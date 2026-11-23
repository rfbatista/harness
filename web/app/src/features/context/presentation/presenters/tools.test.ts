import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, serverJson, toolJson, type SessionContextJson, type ToolJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async (tools: ToolJson[], servers = [serverJson()], usage: SessionContextJson["usage"] = { tools: {}, skills: {} }) => {
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [entryJson({ tools, mcp_servers: servers })], usage })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
  presenter.open();
  await flush();
  return presenter;
};

const mcp = (server: string, tool: string, state = "allowed") => toolJson({ name: `mcp__${server}__${tool}`, source: "mcp", server, state });

describe("Tools", () => {
  test("groups Claude Code first, then each MCP server by name, with counts", async () => {
    const p = await setup(
      [mcp("task", "get_task"), toolJson({ name: "Bash" }), mcp("github", "create_pr"), toolJson({ name: "Read" }), mcp("task", "update_task")],
      [serverJson({ name: "github" }), serverJson({ name: "task" })],
    );
    const group = p.viewModel.groups!.tools;
    assert.deepEqual(group.sections.map((s) => [s.title, s.count]), [["Claude Code", "2"], ["github", "1"], ["task", "2"]]);
    assert.equal(group.count, "5");
    assert.deepEqual(group.sections[2]!.rows.map((r) => r.name), ["get_task", "update_task"]);
    assert.equal(group.sections[2]!.rows[0]!.fullName, "mcp__task__get_task");
  });

  test("each tool states its state in words", async () => {
    const p = await setup([toolJson({ name: "Bash", state: "allowed" }), toolJson({ name: "Edit", state: "on_demand" }), toolJson({ name: "Write", state: "hidden" }), toolJson({ name: "Task", state: "refused" })]);
    assert.deepEqual(p.viewModel.groups!.tools.sections[0]!.rows.map((r) => r.stateLabel), ["allowed", "on demand", "hidden", "refused"]);
  });

  test("answers whether a tool may be used: the push tool's state is on its row", async () => {
    const p = await setup([mcp("github", "push_files", "refused")], [serverJson({ name: "github" })]);
    const row = p.viewModel.groups!.tools.sections[0]!.rows[0]!;
    assert.deepEqual([row.name, row.state], ["push_files", "refused"]);
  });

  test("a used tool says how often; an unused one says nothing", async () => {
    const p = await setup([toolJson({ name: "Bash" }), toolJson({ name: "Read" })], [], { tools: { Bash: { count: 7, last_at: "2026-11-23T21:00:00Z" } }, skills: {} });
    assert.deepEqual(p.viewModel.groups!.tools.sections[0]!.rows.map((r) => r.used), ["used ×7", ""]);
  });

  test("a server's hidden tools appear in its section as hidden, once", async () => {
    const p = await setup([mcp("task", "get_task")], [serverJson({ name: "task", hidden_tools: ["mcp__task__delete_task", "mcp__task__get_task"] })]);
    const rows = p.viewModel.groups!.tools.sections[0]!.rows;
    assert.deepEqual(rows.map((r) => [r.name, r.state]), [["get_task", "allowed"], ["delete_task", "hidden"]]);
  });

  test("a server without tools or hidden tools has no section", async () => {
    const p = await setup([toolJson({ name: "Bash" })], [serverJson({ name: "quiet" })]);
    assert.deepEqual(p.viewModel.groups!.tools.sections.map((s) => s.title), ["Claude Code"]);
  });
});
