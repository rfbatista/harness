import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson } from "../../testing/fixtures";
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

describe("links", () => {
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
