import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, serverJson, type ServerJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async (servers: ServerJson[]) => {
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [entryJson({ mcp_servers: servers })] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
  presenter.open();
  await flush();
  return { presenter, group: () => presenter.viewModel.groups!.servers };
};

describe("MCP server status wording", () => {
  test("unknown with no reason from the server still gives the contract's sentence", async () => {
    const { group } = await setup([serverJson({ status: "unknown", status_reason: "" })]);
    assert.equal(group().rows[0]!.statusLabel, "status unknown");
    assert.equal(group().rows[0]!.reason, "claude does not report it in a terminal session");
  });

  test("unknown never reads as connected or failed, and does not count as a failure", async () => {
    const { group } = await setup([serverJson({ name: "a" }), serverJson({ name: "b", status: "connected", status_reason: "" })]);
    assert.deepEqual(group().rows.map((r) => [r.status, r.failed]), [["unknown", false], ["connected", false]]);
    assert.equal(group().danger, false);
  });

  test("a connected server shows no reason, even if the server sent one", async () => {
    const { group } = await setup([serverJson({ status: "connected", status_reason: "stale text" })]);
    assert.equal(group().rows[0]!.reason, "");
  });

  test("a failed server with no error text has nothing to Copy but is still a failure", async () => {
    const { group } = await setup([serverJson({ status: "failed", status_reason: "" })]);
    assert.deepEqual([group().rows[0]!.failed, group().rows[0]!.canCopy, group().danger], [true, false, true]);
  });

  test("the failure tone stays on the group while a search hides the failed row", async () => {
    const { presenter, group } = await setup([serverJson({ name: "ok", status: "connected", status_reason: "" }), serverJson({ name: "db", status: "failed", status_reason: "boom" })]);
    presenter.search("ok");
    assert.equal(group().count, "1 of 2");
    assert.equal(group().danger, true);
  });

  test("searching the status word finds the servers in that status", async () => {
    const { presenter, group } = await setup([serverJson({ name: "a" }), serverJson({ name: "b", status: "connected", status_reason: "" }), serverJson({ name: "c", status: "failed", status_reason: "x" })]);
    presenter.search("unknown");
    assert.deepEqual(group().rows.map((r) => r.name), ["a"]);
    presenter.search("failed");
    assert.deepEqual(group().rows.map((r) => r.name), ["c"]);
  });

  test("Copy marks the server copied, and only that server", async () => {
    const { presenter, group } = await setup([serverJson({ name: "a", status: "failed", status_reason: "e1" }), serverJson({ name: "b", status: "failed", status_reason: "e2" })]);
    await presenter.copyError("a");
    assert.deepEqual(group().rows.map((r) => r.copied), [true, false]);
  });
});
