import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, hookJson, pieceJson, serverJson, skillJson, toolJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async () => {
  const entry = entryJson({
    instructions: [pieceJson({ id: "a", title: "Task brief", text: "Build the Context tab." }), pieceJson({ id: "b", title: "Branch gate", text: "Never push without approval." })],
    tools: [toolJson({ name: "Bash" }), toolJson({ name: "Read" }), toolJson({ name: "mcp__github__push_files", source: "mcp", server: "github", state: "refused" }), toolJson({ name: "mcp__github__list_prs", source: "mcp", server: "github" })],
    mcp_servers: [serverJson({ name: "github", attached_reason: "Agent react-developer" }), serverJson({ name: "task" })],
    skills: [skillJson({ name: "task-tdd", description: "Build a ticket test first." }), skillJson({ name: "deslope", description: "Clean up slop." })],
    hooks: [hookJson({ rule: "Rewrites commands through rtk" }), hookJson({ event: "PostToolUse", matcher: "Edit", rule: "Runs the formatter" })],
  });
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [entry] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
  presenter.open();
  await flush();
  const g = () => presenter.viewModel.groups!;
  return { presenter, g };
};

describe("Search", () => {
  test("filters rows in every group and shows filtered counts on the headings", async () => {
    const { presenter, g } = await setup();
    presenter.search("push");
    assert.deepEqual([g().instructions.count, g().tools.count, g().servers.count, g().skills.count, g().hooks.count], ["1 of 2", "1 of 4", "0 of 2", "0 of 2", "0 of 2"]);
    assert.deepEqual(g().tools.sections.map((s) => [s.title, s.count, s.rows.map((r) => r.name)]), [["github", "1 of 2", ["push_files"]]]);
  });

  test("searches a name, a description, a rule and a server's reason, without regard to case", async () => {
    const { presenter, g } = await setup();
    presenter.search("TDD");
    assert.deepEqual(g().skills.rows.map((r) => r.name), ["task-tdd"]);
    presenter.search("formatter");
    assert.deepEqual(g().hooks.rows.map((r) => r.event), ["PostToolUse"]);
    presenter.search("react-developer");
    assert.deepEqual(g().servers.rows.map((r) => r.name), ["github"]);
  });

  test("a server's name finds all of its tools", async () => {
    const { presenter, g } = await setup();
    presenter.search("github");
    assert.deepEqual(g().tools.sections[0]!.rows.map((r) => r.name), ["push_files", "list_prs"]);
  });

  test("a sentence inside an instruction keeps that piece and marks it matched in its text", async () => {
    const { presenter, g } = await setup();
    presenter.search("without approval");
    assert.deepEqual(g().instructions.rows.map((r) => [r.id, r.matchedInText]), [["b", true]]);
    presenter.search("brief");
    assert.deepEqual(g().instructions.rows.map((r) => [r.id, r.matchedInText]), [["a", false]]);
  });

  test("a group with no match says so and keeps its heading", async () => {
    const { presenter, g } = await setup();
    presenter.search("zzz");
    assert.equal(g().tools.noMatch, true);
    assert.equal(g().tools.count, "0 of 4");
    assert.equal(g().tools.title, "Tools");
  });

  test("clearing restores every row, in its order, with plain counts", async () => {
    const { presenter, g } = await setup();
    const before = JSON.stringify(g());
    presenter.search("push");
    presenter.clearSearch();
    assert.equal(JSON.stringify(g()), before);
    assert.equal(presenter.viewModel.search.query, "");
  });

  test("a blank query is no query", async () => {
    const { presenter, g } = await setup();
    presenter.search("   ");
    assert.equal(g().tools.count, "4");
  });

  test("asking for the search box raises a focus request the view acts on", async () => {
    const { presenter } = await setup();
    const before = presenter.viewModel.search.focusRequest;
    presenter.focusSearch();
    assert.equal(presenter.viewModel.search.focusRequest, before + 1);
  });

  test("rows do not reorder while searching", async () => {
    const { presenter, g } = await setup();
    presenter.search("o");
    const order = g().tools.sections.flatMap((s) => s.rows.map((r) => r.fullName));
    assert.deepEqual(order, ["Bash", "Read", "mcp__github__push_files", "mcp__github__list_prs"].filter((n) => order.includes(n)));
  });
});
