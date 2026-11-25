import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { BoardSourceError } from "../../application/ports";
import type { SessionBranches } from "../../domain/branches";
import { memoryStorage } from "../../testing/memory-storage";
import { AGENT_NAMES, T0, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const s1 = session({ branch: "agent/x" });
const list = (pushes: number, extra: SessionBranches["branches"] = []): SessionBranches => ({ sessionId: s1.id, branches: [{ name: "agent/x", isSessionBranch: true, state: "session", pushes }, ...extra] });

async function board(branches: SessionBranches[], fail = false) {
  const source = memoryBoardSource([s1]);
  source.setBranches(branches);
  if (fail) source.branches = async () => Promise.reject(new BoardSourceError("NETWORK_ERROR", "offline"));
  const presenter = new BoardPresenter({ source, preferences: storageBoardPreferences(memoryStorage()), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => new Date(T0) });
  await presenter.start();
  presenter.follow();
  return { presenter, source };
}

const card = (p: BoardPresenter) => p.viewModel.cards.find((c) => c.id === s1.id)!;

describe("push state on a card", () => {
  test("says pushed once a push of the session's branch is recorded, and not pushed before", async () => {
    assert.equal(card((await board([list(2)])).presenter).pushState, "pushed");
    assert.equal(card((await board([list(0)])).presenter).pushState, "not pushed");
  });

  test("says nothing for a session with no branch list, and flags unapproved branches", async () => {
    assert.equal(card((await board([])).presenter).pushState, "");
    const flagged = await board([list(0, [{ name: "other", isSessionBranch: false, state: "unapproved", pushes: 0 }])]);
    assert.equal(card(flagged.presenter).unapproved, 1);
  });

  test("a Branches list that cannot be read leaves the card without it, not the board without cards", async () => {
    const { presenter } = await board([], true);
    assert.equal(presenter.viewModel.phase, "ready");
    assert.equal(card(presenter).pushState, "");
  });

  test("follows the feed: a push updates the card", async () => {
    const { presenter, source } = await board([list(0)]);
    source.emit({ kind: "branches", branches: list(1) });
    assert.equal(card(presenter).pushState, "pushed");
  });
});
