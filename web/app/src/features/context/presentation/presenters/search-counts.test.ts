import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, hookJson, serverJson, skillJson, toolJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async () => {
  const entry = entryJson({
    tools: [
      toolJson({ name: "Bash" }),
      toolJson({ name: "Read", state: "on_demand" }),
      toolJson({ name: "mcp__github__push_files", source: "mcp", server: "github", state: "refused" }),
      toolJson({ name: "mcp__github__list_prs", source: "mcp", server: "github" }),
      toolJson({ name: "mcp__task__get_task", source: "mcp", server: "task" }),
    ],
    mcp_servers: [serverJson({ name: "github", hidden_tools: ["mcp__github__delete_repo"] }), serverJson({ name: "task" })],
    skills: [skillJson({ name: "alpha" })],
    hooks: [hookJson()],
  });
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [entry], changes: [{ at: "2026-11-23T21:05:00Z", kind: "skill_invoked", subject: "alpha", detail: "from the prompt" }, { at: "2026-11-23T21:04:00Z", kind: "mcp_failed", subject: "github", detail: "boom" }] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), timeZone: "UTC" });
  presenter.open();
  await flush();
  return { presenter, g: () => presenter.viewModel.groups! };
};

describe("search counts", () => {
  test("tool section counts follow the filter, each against its own total", async () => {
    const { presenter, g } = await setup();
    presenter.search("push");
    assert.equal(g().tools.count, "1 of 6");
    assert.deepEqual(g().tools.sections.map((s) => [s.title, s.count]), [["github", "1 of 3"]]);
  });

  test("a tool's state word finds it", async () => {
    const { presenter, g } = await setup();
    presenter.search("on demand");
    assert.deepEqual(g().tools.sections.flatMap((s) => s.rows.map((r) => r.fullName)), ["Read"]);
    presenter.search("hidden");
    assert.deepEqual(g().tools.sections.flatMap((s) => s.rows.map((r) => r.fullName)), ["mcp__github__delete_repo"]);
  });

  test("Changes are searched by their words and their note, and counted", async () => {
    const { presenter, g } = await setup();
    presenter.search("boom");
    assert.equal(g().changes.count, "1 of 2");
    presenter.search("skill alpha");
    assert.equal(g().changes.count, "1 of 2");
  });

  test("a group that holds no match keeps its heading and a 0 of N count", async () => {
    const { presenter, g } = await setup();
    presenter.search("zzz");
    for (const group of [g().instructions, g().tools, g().servers, g().skills, g().hooks, g().changes]) {
      assert.equal(group.noMatch, true);
      assert.match(group.count, /^0 of \d+$/);
    }
    assert.equal(g().tools.count, "0 of 6");
  });

  test("the search never opens or closes a group", async () => {
    const { presenter, g } = await setup();
    presenter.search("rtk");
    assert.equal(g().hooks.open, false);
    assert.equal(g().tools.open, true);
  });

  test("Esc clears the query and restores every count", async () => {
    const { presenter, g } = await setup();
    presenter.search("push");
    presenter.clearSearch();
    assert.equal(presenter.viewModel.search.query, "");
    assert.deepEqual([g().tools.count, g().servers.count, g().changes.count], ["6", "2", "2"]);
  });
});
