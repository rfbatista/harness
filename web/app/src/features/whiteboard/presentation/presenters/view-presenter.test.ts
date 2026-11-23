import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { storageBoardPreferences, viewKey } from "../../infrastructure/storage-board-preferences";
import { memoryPage } from "../../testing/memory-page";
import { memoryStorage } from "../../testing/memory-storage";
import { ViewPresenter } from "./view-presenter";

function page(search = "", storage = memoryStorage(), ticketId = "t1") {
  const memory = memoryPage();
  const presenter = new ViewPresenter({ ticketId, preferences: storageBoardPreferences(storage), host: memory.viewHost, signals: memory.signals, search });
  presenter.start();
  return { presenter, memory, storage };
}

describe("the view of a task's sessions", () => {
  test("is the list by default", () => {
    const { presenter, memory } = page();
    assert.equal(presenter.viewModel.view, "list");
    assert.deepEqual(memory.shown, [{ view: "list", session: "" }]);
  });

  test("is kept per task when the person chooses, and restored the next time", () => {
    const first = page();
    first.memory.choose("whiteboard");
    assert.equal(first.presenter.viewModel.view, "whiteboard");
    assert.equal(first.storage.entries.get(viewKey("t1")), "whiteboard");
    const next = page("", first.storage);
    assert.equal(next.presenter.viewModel.view, "whiteboard");
    assert.deepEqual(next.memory.shown, [{ view: "whiteboard", session: "" }]);
    first.memory.choose("list");
    assert.equal(page("", first.storage).presenter.viewModel.view, "list");
  });

  test("one task's choice never reaches another", () => {
    const first = page();
    first.memory.choose("whiteboard");
    assert.equal(page("", first.storage, "t2").presenter.viewModel.view, "list");
    assert.equal(first.storage.entries.has(viewKey("t2")), false);
  });

  test("an unreadable stored choice gives the list", () => {
    for (const text of ["", "board", "{}", "null"]) assert.equal(page("", memoryStorage({ [viewKey("t1")]: text })).presenter.viewModel.view, "list");
    const blocked = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); }, removeItem: () => { throw new Error("blocked"); } };
    const p = page("", blocked as never);
    p.memory.choose("whiteboard");
    assert.equal(p.presenter.viewModel.view, "whiteboard");
  });

  test("view=whiteboard&session=<id> opens the board with that session, and keeps no choice", () => {
    const { presenter, memory, storage } = page("?view=whiteboard&session=s7");
    assert.equal(presenter.viewModel.view, "whiteboard");
    assert.deepEqual(memory.shown, [{ view: "whiteboard", session: "s7" }]);
    assert.equal(storage.entries.has(viewKey("t1")), false);
  });

  test("the link wins over what was kept; a link to the list wins too", () => {
    const kept = memoryStorage({ [viewKey("t1")]: "list" });
    assert.equal(page("?view=whiteboard", kept).presenter.viewModel.view, "whiteboard");
    const board = memoryStorage({ [viewKey("t1")]: "whiteboard" });
    assert.equal(page("?view=list&session=s1", board).presenter.viewModel.view, "list");
    assert.deepEqual(page("?view=list&session=s1", board).memory.shown, [{ view: "list", session: "" }]);
  });

  test("a session without view=whiteboard, or an unknown view, selects nothing on the board", () => {
    assert.deepEqual(page("?session=s1").memory.shown, [{ view: "list", session: "" }]);
    assert.deepEqual(page("?view=timeline&session=s1").memory.shown, [{ view: "list", session: "" }]);
  });

  test("stop stops listening", () => {
    const { presenter, memory } = page();
    presenter.stop();
    assert.equal(memory.followers(), 0);
  });
});
