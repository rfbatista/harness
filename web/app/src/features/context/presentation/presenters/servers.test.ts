import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, serverJson, type ServerJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async (servers: ServerJson[]) => {
  const source = memoryContextSource();
  const clipboard = memoryClipboard();
  source.serve(contextFromJson(contextJson({ entries: [entryJson({ mcp_servers: servers })] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard });
  presenter.open();
  await flush();
  return { presenter, clipboard, group: () => presenter.viewModel.groups!.servers };
};

describe("MCP servers", () => {
  test("lists each server with how and why it was attached", async () => {
    const { group } = await setup([
      serverJson({ name: "task", attached: "built_in", attached_reason: "Every sandboxed session (ADR 0025)" }),
      serverJson({ name: "rtk", attached: "token_saver", attached_reason: "Token saver rtk" }),
      serverJson({ name: "github", attached: "agent", attached_reason: "Agent react-developer" }),
      serverJson({ name: "postgres", attached: "project", attached_reason: "Project MCP settings" }),
      serverJson({ name: "browser", attached: "mode", attached_reason: "Mode implement" }),
    ]);
    assert.deepEqual(group().rows.map((r) => [r.name, r.attached, r.attachedReason]), [
      ["task", "Built in", "Every sandboxed session (ADR 0025)"],
      ["rtk", "Token saver", "Token saver rtk"],
      ["github", "Agent", "Agent react-developer"],
      ["postgres", "Project", "Project MCP settings"],
      ["browser", "Mode", "Mode implement"],
    ]);
    assert.equal(group().count, "5");
  });

  test("a connected server says so and has no failure", async () => {
    const { group } = await setup([serverJson({ status: "connected", status_reason: "" })]);
    assert.deepEqual([group().rows[0]!.statusLabel, group().rows[0]!.failed, group().danger], ["connected", false, false]);
  });

  test("a failed server is a failure on its row and on the group count, with claude's error", async () => {
    const { group } = await setup([serverJson({ name: "ok", status: "connected", status_reason: "" }), serverJson({ name: "db", status: "failed", status_reason: "spawn ENOENT" })]);
    assert.equal(group().danger, true);
    assert.deepEqual(group().rows.map((r) => r.failed), [false, true]);
    assert.equal(group().rows[1]!.reason, "spawn ENOENT");
    assert.equal(group().rows[1]!.canCopy, true);
    assert.equal(group().rows[0]!.canCopy, false);
  });

  test("Copy puts exactly claude's error on the clipboard", async () => {
    const { presenter, clipboard, group } = await setup([serverJson({ name: "db", status: "failed", status_reason: "spawn ENOENT\n  at x" })]);
    await presenter.copyError("db");
    assert.deepEqual(clipboard.copied, ["spawn ENOENT\n  at x"]);
    assert.equal(group().rows[0]!.copied, true);
  });

  test("Copy does nothing for a server that did not fail", async () => {
    const { presenter, clipboard } = await setup([serverJson({ name: "task" })]);
    await presenter.copyError("task");
    assert.deepEqual(clipboard.copied, []);
  });

  test("an unknown status shows the contract's reason and is not a failure", async () => {
    const { group } = await setup([serverJson({ status: "unknown", status_reason: "claude does not report it in a terminal session" })]);
    const row = group().rows[0]!;
    assert.deepEqual([row.statusLabel, row.reason, row.failed, row.canCopy, group().danger], ["status unknown", "claude does not report it in a terminal session", false, false, false]);
  });

  test("no other group carries the danger tone", async () => {
    const { presenter } = await setup([serverJson({ status: "failed", status_reason: "boom" })]);
    const { instructions, tools } = presenter.viewModel.groups!;
    assert.equal("danger" in instructions || "danger" in tools, false);
  });
});
