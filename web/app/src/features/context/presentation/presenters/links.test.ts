import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson, serverJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async (role: string, whiteboardHref?: string) => {
  const source = memoryContextSource();
  source.serve(contextFromJson(contextJson({ entries: [entryJson({ facts: factsJson({ role }) })] })));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), whiteboardHref });
  presenter.open();
  await flush();
  return presenter;
};

describe("settings links", () => {
  const withEntry = async (entry: ReturnType<typeof entryJson>, projectId = "p1") => {
    const source = memoryContextSource();
    source.serve(contextFromJson(contextJson({ session_id: "s 1", entries: [entry] })));
    const p = new ContextPresenter(source, "s 1", { clipboard: memoryClipboard(), projectId });
    p.open();
    await flush();
    return p.viewModel.links.map((l) => [l.label, l.href]);
  };

  test("point at the pages where the agent, its servers and the project's MCP settings are changed", async () => {
    const links = await withEntry(
      entryJson({
        facts: factsJson({ agent: "react developer" }),
        mcp_servers: [serverJson({ name: "github", attached: "agent" }), serverJson({ name: "postgres", attached: "project" }), serverJson({ name: "rtk", attached: "token_saver" }), serverJson({ name: "task", attached: "built_in" })],
      }),
    );
    assert.deepEqual(links, [
      ["Agent react developer in the catalog", "/agents/react%20developer"],
      ["MCP server github in the catalog", "/agents/mcp/github"],
      ["Project MCP settings", "/projects/p1/settings"],
      ["Token savers", "/token-savers"],
      ["Sandbox tab", "?session=s%201&tab=sandbox"],
      ["History", "?session=s%201&tab=history"],
    ]);
  });

  test("a target with no page has no link: no agent, no project, no such server", async () => {
    const links = await withEntry(entryJson({ facts: factsJson({ agent: "" }), mcp_servers: [serverJson({ attached: "project" }), serverJson({ attached: "mode" })] }), "");
    assert.deepEqual(links.map(([label]) => label), ["Sandbox tab", "History"]);
  });
});

describe("links", () => {
  test("names the parent agent on the whiteboard link", async () => {
    const source = memoryContextSource();
    source.serve(contextFromJson(contextJson({ entries: [entryJson({ facts: factsJson({ role: "delegate" }) })] })));
    const p = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), whiteboardHref: "/w", parent: "software-architect" });
    p.open();
    await flush();
    assert.equal(p.viewModel.whiteboard?.label, "Delegate of software-architect · see on the whiteboard");
  });

  test("a delegate links to the whiteboard with itself selected, once the whiteboard's address is known", async () => {
    const p = await setup("delegate", "/projects/p1/tasks/t1?view=whiteboard&session=s1");
    assert.deepEqual(p.viewModel.whiteboard, { label: "Delegate · see on the whiteboard", href: "/projects/p1/tasks/t1?view=whiteboard&session=s1" });
  });

  test("the link is hidden until the whiteboard exists, and for a session that is not a delegate", async () => {
    assert.equal((await setup("delegate")).viewModel.whiteboard, null);
    assert.equal((await setup("architect", "/x")).viewModel.whiteboard, null);
    assert.equal((await setup("", "/x")).viewModel.whiteboard, null);
  });

  test("a link to a group opens it, even closed, and asks the view to bring it into sight", async () => {
    const p = await setup("delegate");
    p.toggleGroup("hooks");
    p.toggleGroup("hooks");
    p.openAt("hooks");
    assert.equal(p.viewModel.groups!.hooks.open, true);
    assert.deepEqual(p.viewModel.reveal, { group: "hooks", request: 1 });
    p.openAt("hooks");
    assert.equal(p.viewModel.reveal!.request, 2);
  });

  test("a link that arrives before the context has loaded is honoured once it has", async () => {
    const source = memoryContextSource();
    source.serve(contextFromJson(contextJson()));
    const p = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
    p.openAt("hooks");
    p.open();
    await flush();
    assert.equal(p.viewModel.groups!.hooks.open, true);
    assert.equal(p.viewModel.reveal?.group, "hooks");
  });
});

describe("the focus request", () => {
  test("is asked for once per ask, and a presenter made later asks for none", async () => {
    const source = memoryContextSource();
    source.serve(contextFromJson(contextJson()));
    const first = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
    first.open();
    await flush();
    assert.equal(first.viewModel.search.focusRequest, 0);
    first.focusSearch();
    assert.equal(first.viewModel.search.focusRequest, 1);
    // The pane remounts the island (another session, live to ended): a new presenter does not inherit the ask.
    const second = new ContextPresenter(source, "s1", { clipboard: memoryClipboard() });
    second.open();
    await flush();
    assert.equal(second.viewModel.search.focusRequest, 0);
  });
});
