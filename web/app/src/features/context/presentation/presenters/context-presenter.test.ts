import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ContextSourceError } from "../../application/ports";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const setup = (json = contextJson()) => {
  const source = memoryContextSource();
  source.serve(contextFromJson(json));
  return { source, presenter: new ContextPresenter(source, json.session_id, { clipboard: memoryClipboard() }) };
};

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
const fact = (presenter: ContextPresenter, label: string) => presenter.viewModel.facts.find((f) => f.label === label)?.value;

describe("the Context tab's load", () => {
  test("is idle until the tab is opened, then loading, then ready", async () => {
    const { source, presenter } = setup();
    assert.equal(presenter.viewModel.status, "idle");
    const release = source.hold();
    presenter.open();
    assert.equal(presenter.viewModel.status, "loading");
    release();
    await flush();
    assert.equal(presenter.viewModel.status, "ready");
  });

  test("fetches on first show only: opening again does not refetch", async () => {
    const { source, presenter } = setup();
    presenter.open();
    await flush();
    presenter.open();
    await flush();
    assert.deepEqual(source.loaded, ["s1"]);
  });

  test("a session without recorded context says so and shows no facts", async () => {
    const { presenter } = setup(contextJson({ recorded: false, entries: [] }));
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.status, "unrecorded");
    assert.equal(presenter.viewModel.message, "This session started before the harness recorded its context");
    assert.deepEqual(presenter.viewModel.facts, []);
  });

  test("a session the server does not know is its own state", async () => {
    const source = memoryContextSource();
    const presenter = new ContextPresenter(source, "gone", { clipboard: memoryClipboard() });
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.status, "not_found");
  });

  test("a failed load is an error that Retry loads again", async () => {
    const { source, presenter } = setup();
    source.failWith(new ContextSourceError("NETWORK_ERROR", "offline"));
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.status, "error");
    source.heal();
    presenter.retry();
    await flush();
    assert.equal(presenter.viewModel.status, "ready");
  });

  test("a bad answer is told apart from a network failure", async () => {
    const { source, presenter } = setup();
    source.failWith(new ContextSourceError("BAD_RESPONSE", "SessionContextDto.entries: expected an array"));
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.status, "error");
    assert.match(presenter.viewModel.message, /unexpected answer/);
  });
});

describe("the facts grid", () => {
  test("shows the latest entry's facts", async () => {
    const { presenter } = setup(
      contextJson({
        entries: [entryJson({ id: "e1", facts: factsJson({ branch: "old" }) }), entryJson({ id: "e2", kind: "resume", facts: factsJson({ branch: "new" }) })],
      }),
    );
    presenter.open();
    await flush();
    assert.equal(fact(presenter, "Worktree & branch"), "/work/harness-1 · new");
  });

  test("reads each setup fact at a glance", async () => {
    const { presenter } = setup();
    presenter.open();
    await flush();
    assert.deepEqual(presenter.viewModel.facts, [
      { label: "Agent", value: "react-developer · delegate" },
      { label: "Mode & model", value: "implement · claude-sonnet-5-5" },
      { label: "Session", value: "Terminal session" },
      { label: "Permissions", value: "acceptEdits" },
      { label: "Sandbox & visibility", value: "seatbelt · read all" },
      { label: "Worktree & branch", value: "/work/harness-1 · agent/context-tab-1" },
      { label: "Token savers", value: "rtk" },
      { label: "Claude version", value: "2.1.4" },
      { label: "Approximate size", value: "≈5.2k tokens" },
    ]);
  });

  test("names what is empty instead of leaving blanks", async () => {
    const { presenter } = setup(
      contextJson({ entries: [entryJson({ facts: factsJson({ agent: "", role: "", mode: "", headless: true, token_savers: [], claude_version: "", approx_tokens: 640, sandbox: "off" }) })] }),
    );
    presenter.open();
    await flush();
    assert.equal(fact(presenter, "Agent"), "Plain claude");
    assert.equal(fact(presenter, "Mode & model"), "No mode · claude-sonnet-5-5");
    assert.equal(fact(presenter, "Session"), "Headless session");
    assert.equal(fact(presenter, "Sandbox & visibility"), "off · read all");
    assert.equal(fact(presenter, "Token savers"), "none");
    assert.equal(fact(presenter, "Claude version"), "unknown");
    assert.equal(fact(presenter, "Approximate size"), "≈640 tokens");
  });
});

describe("empty parts of a fact", () => {
  test("leave no dangling separator", async () => {
    const { presenter } = setup(contextJson({ entries: [entryJson({ facts: factsJson({ model: "", mode: "design", sandbox: "off", repository_visibility: "", worktree: "", branch: "main" }) })] }));
    presenter.open();
    await flush();
    assert.equal(fact(presenter, "Mode & model"), "design");
    assert.equal(fact(presenter, "Sandbox & visibility"), "off");
    assert.equal(fact(presenter, "Worktree & branch"), "main");
  });

  test("no mode and no model reads as no mode", async () => {
    const { presenter } = setup(contextJson({ entries: [entryJson({ facts: factsJson({ model: "", mode: "" }) })] }));
    presenter.open();
    await flush();
    assert.equal(fact(presenter, "Mode & model"), "No mode");
  });
});
