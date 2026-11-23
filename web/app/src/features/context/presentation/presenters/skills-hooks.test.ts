import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, hookJson, skillJson, type SessionContextJson, type SkillJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const use = (count: number) => ({ count, last_at: "2026-11-23T21:00:00Z" });

const setup = async (skills: SkillJson[], usage: SessionContextJson["usage"] = { tools: {}, skills: {} }, hooks = [hookJson()]) => {
  const source = memoryContextSource();
  const json = contextJson({ entries: [entryJson({ skills, hooks })], usage });
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
  presenter.open();
  await flush();
  return { source, presenter, json, names: () => presenter.viewModel.groups!.skills.rows.map((r) => r.name) };
};

const A = skillJson({ name: "alpha", source: "repository" });
const B = skillJson({ name: "beta", source: "agent" });
const C = skillJson({ name: "gamma", source: "built_in" });

describe("Skills", () => {
  test("shows each skill with its source and description, with the count on the heading", async () => {
    const { presenter } = await setup([skillJson({ name: "task-tdd", source: "mode", description: "Build a ticket test first." })]);
    const g = presenter.viewModel.groups!.skills;
    assert.deepEqual(g.rows[0], { name: "task-tdd", source: "Mode", description: "Build a ticket test first.", invoked: "", ringing: false });
    assert.equal(g.count, "1");
    assert.deepEqual([A, B, C].map((s) => s.source), ["repository", "agent", "built_in"]);
  });

  test("the source of each skill is named", async () => {
    const { presenter } = await setup([A, B, C, skillJson({ name: "delta", source: "mode" })]);
    assert.deepEqual(presenter.viewModel.groups!.skills.rows.map((r) => r.source), ["Repository", "Agent", "Built in", "Mode"]);
  });

  test("invoked skills come first with their count, most used first; the rest keep the entry's order", async () => {
    const { names, presenter } = await setup([A, B, C, skillJson({ name: "delta" })], { tools: {}, skills: { gamma: use(5), delta: use(2) } });
    assert.deepEqual(names(), ["gamma", "delta", "alpha", "beta"]);
    assert.deepEqual(presenter.viewModel.groups!.skills.rows.map((r) => r.invoked), ["invoked ×5", "invoked ×2", "", ""]);
  });

  test("a later invocation updates the count and never reorders the rows", async () => {
    const { source, presenter, json, names } = await setup([A, B, C], { tools: {}, skills: { gamma: use(1) } });
    assert.deepEqual(names(), ["gamma", "alpha", "beta"]);
    source.serve(contextFromJson({ ...json, usage: { tools: {}, skills: { gamma: use(1), alpha: use(9) } } }));
    await presenter.refresh();
    assert.deepEqual(names(), ["gamma", "alpha", "beta"]);
    assert.equal(presenter.viewModel.groups!.skills.rows[1]!.invoked, "invoked ×9");
  });
});

describe("Hooks", () => {
  test("are collapsed by default and list event, matcher and rule only", async () => {
    const { presenter } = await setup([], undefined, [hookJson({ event: "PreToolUse", matcher: "Bash", rule: "Rewrites commands through rtk" })]);
    const g = presenter.viewModel.groups!.hooks;
    assert.equal(g.open, false);
    assert.equal(g.count, "1");
    assert.deepEqual(g.rows, [{ event: "PreToolUse", matcher: "Bash", rule: "Rewrites commands through rtk" }]);
    presenter.toggleGroup("hooks");
    assert.equal(presenter.viewModel.groups!.hooks.open, true);
  });
});
