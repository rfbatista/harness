import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import { recordingHost } from "../../testing/recording-host";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, message, review, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const NOW = new Date(T0 + 3_600_000);

const architect = session({ role: "architect", agentId: "arch" });
const impl1 = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const rev = session({ role: "delegate", mode: "review", agentId: "rev", parentSessionId: architect.id });
const impl2 = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const sub = session({ role: "delegate", mode: "test", parentSessionId: impl1.id });

async function board(sessions: BoardSession[] = [architect, impl1, rev, impl2, sub], tweak?: (source: ReturnType<typeof memoryBoardSource>) => void) {
  const source = memoryBoardSource(sessions);
  tweak?.(source);
  const host = recordingHost();
  const presenter = new BoardPresenter({ source, preferences: storageBoardPreferences(memoryStorage()), host, ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => NOW });
  await presenter.start();
  return { presenter, host };
}

const order = (p: BoardPresenter) => p.viewModel.cards.map((c) => c.id);
const press = (p: BoardPresenter, key: string) => p.key({ key });

describe("walking the graph", () => {
  test("the cards are in rank order: You, the architect, then column by column, top to bottom", async () => {
    const { presenter } = await board();
    assert.deepEqual(order(presenter), ["you", architect.id, impl1.id, impl2.id, rev.id, sub.id]);
  });

  test("Down and Up move within a column and stop at the ends", async () => {
    const { presenter } = await board();
    presenter.focus(impl1.id);
    press(presenter, "ArrowDown");
    assert.equal(presenter.viewModel.focusedId, impl2.id);
    press(presenter, "ArrowDown");
    assert.equal(presenter.viewModel.focusedId, impl2.id);
    press(presenter, "ArrowUp");
    press(presenter, "ArrowUp");
    assert.equal(presenter.viewModel.focusedId, impl1.id);
  });

  test("Right goes from a card to the first session it started, Left back to the one that started it", async () => {
    const { presenter } = await board();
    presenter.focus("you");
    press(presenter, "ArrowRight");
    assert.equal(presenter.viewModel.focusedId, architect.id);
    press(presenter, "ArrowRight");
    assert.equal(presenter.viewModel.focusedId, impl1.id);
    press(presenter, "ArrowRight");
    assert.equal(presenter.viewModel.focusedId, sub.id);
    press(presenter, "ArrowRight");
    assert.equal(presenter.viewModel.focusedId, sub.id);
    press(presenter, "ArrowLeft");
    press(presenter, "ArrowLeft");
    press(presenter, "ArrowLeft");
    assert.equal(presenter.viewModel.focusedId, "you");
    press(presenter, "ArrowLeft");
    assert.equal(presenter.viewModel.focusedId, "you");
  });

  test("with no card focused the arrows pan the camera", async () => {
    const { presenter } = await board();
    const { x } = presenter.viewModel.camera;
    press(presenter, "ArrowRight");
    assert.notEqual(presenter.viewModel.camera.x, x);
  });
});

describe("opening", () => {
  test("Enter selects the focused card and asks the page to open its pane; the cards go compact", async () => {
    const { presenter, host } = await board();
    presenter.focus(rev.id);
    press(presenter, "Enter");
    assert.deepEqual(host.calls, [`open ${rev.id}`]);
    assert.equal(presenter.viewModel.selectedId, rev.id);
    assert.equal(presenter.viewModel.compact, true);
    assert.equal(presenter.viewModel.cards.find((c) => c.id === rev.id)?.selected, true);
  });

  test("C asks for the card's Context tab", async () => {
    const { presenter, host } = await board();
    presenter.focus(rev.id);
    press(presenter, "c");
    assert.deepEqual(host.calls, [`open ${rev.id} context`]);
    assert.equal(presenter.viewModel.selectedId, rev.id);
  });

  test("Escape closes the pane, clears the selection and returns focus to the canvas", async () => {
    const { presenter, host } = await board();
    presenter.focus(rev.id);
    press(presenter, "Enter");
    press(presenter, "Escape");
    assert.deepEqual(host.calls, [`open ${rev.id}`, "close"]);
    assert.equal(presenter.viewModel.selectedId, null);
    assert.equal(presenter.viewModel.focusedId, null);
    assert.equal(presenter.viewModel.compact, false);
  });

  test("the You card opens no pane: Enter goes to the Reviews popover while a review waits, and nothing otherwise", async () => {
    const quiet = await board();
    quiet.presenter.focus("you");
    press(quiet.presenter, "Enter");
    press(quiet.presenter, "c");
    assert.deepEqual(quiet.host.calls, []);
    const waiting = await board(undefined, (s) => s.setReviews([review(architect.id)]));
    waiting.presenter.focus("you");
    press(waiting.presenter, "Enter");
    assert.deepEqual(waiting.host.calls, ["reviews"]);
    assert.equal(waiting.presenter.viewModel.selectedId, null);
  });

  test("clicking a message pill selects the delegate on its Conversation tab; the You pill opens Reviews", async () => {
    const { presenter, host } = await board(undefined, (s) => {
      s.setMessages([message(rev.id, architect.id, { kind: "status_report" })]);
      s.setReviews([review(architect.id)]);
    });
    presenter.openThread(`${architect.id}>${rev.id}`);
    presenter.openThread(`you>${architect.id}`);
    assert.deepEqual(host.calls, [`open ${rev.id} conversation`, "reviews"]);
  });

  test("a selection the page makes (the list, or the URL) shows without asking the page again", async () => {
    const { presenter, host } = await board();
    presenter.selectFromPage(sub.id);
    assert.equal(presenter.viewModel.selectedId, sub.id);
    assert.deepEqual(host.calls, []);
    presenter.selectFromPage(null);
    assert.equal(presenter.viewModel.selectedId, null);
  });

  test("a selection of a session that is not on the board is dropped", async () => {
    const { presenter } = await board();
    presenter.selectFromPage("nope");
    assert.equal(presenter.viewModel.selectedId, null);
  });
});

describe("names and states", () => {
  test("a card's name reads like the row's tooltip and the canvas label counts the sessions and names the keys", async () => {
    const { presenter } = await board();
    assert.equal(presenter.viewModel.cards.find((c) => c.id === rev.id)?.name, "code-reviewer, review, running");
    assert.equal(presenter.viewModel.canvasLabel, "5 sessions as a graph. Tab moves between agents, arrows walk the tree, Enter opens a session, C opens its Context tab.");
    assert.equal((await board([architect])).presenter.viewModel.canvasLabel.startsWith("1 session as a graph."), true);
  });

  test("a task with no session shows the empty state with You still present", async () => {
    const { presenter } = await board([]);
    assert.equal(presenter.viewModel.state, "empty");
    assert.deepEqual(order(presenter), ["you"]);
    assert.equal(presenter.viewModel.endedNote, "");
  });

  test("a task whose sessions have all ended shows the quiet ended state, dimmed and still selectable", async () => {
    const ended = [architect, impl1].map((s) => ({ ...s, status: "done" as const }));
    const { presenter, host } = await board(ended);
    assert.equal(presenter.viewModel.state, "ended");
    assert.equal(presenter.viewModel.endedNote, "All 2 sessions ended · the picture stays as it was");
    assert.ok(presenter.viewModel.cards.filter((c) => c.kind === "session").every((c) => c.dimmed));
    presenter.focus(impl1.id);
    press(presenter, "Enter");
    assert.deepEqual(host.calls, [`open ${impl1.id}`]);
  });

  test("with a running session the board is live", async () => {
    assert.equal((await board()).presenter.viewModel.state, "live");
  });

  test("the empty state's actions go to the page", async () => {
    const { presenter, host } = await board([]);
    presenter.newSession();
    presenter.showList();
    assert.deepEqual(host.calls, ["new", "list"]);
  });
});
