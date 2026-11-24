import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { browserPageBridge } from "./browser-page-bridge";

const windowLike = () => new EventTarget() as unknown as Window;

const listen = (win: Window, name: string): unknown[] => {
  const seen: unknown[] = [];
  win.addEventListener(name, (e) => seen.push((e as CustomEvent).detail));
  return seen;
};

describe("browser page bridge", () => {
  test("tells the page the view, the pane and the person's other places, as DTOs on the window", () => {
    const win = windowLike();
    const { viewHost, boardHost } = browserPageBridge(win);
    const seen = { view: listen(win, "whiteboard-view"), open: listen(win, "whiteboard-open"), close: listen(win, "whiteboard-close"), reviews: listen(win, "whiteboard-reviews"), ready: listen(win, "whiteboard-ready") };
    viewHost.showView("whiteboard", "s1");
    boardHost.openSession("s2", "context");
    boardHost.openSession("s3");
    boardHost.closePane();
    boardHost.openReviews();
    boardHost.boardReady();
    assert.deepEqual(seen.view, [{ view: "whiteboard", session_id: "s1" }]);
    assert.deepEqual(seen.open, [{ session_id: "s2", tab: "context" }, { session_id: "s3", tab: "" }]);
    assert.equal(seen.close.length + seen.reviews.length + seen.ready.length, 3);
  });

  test("follows the person's view choice and the page's selection, and stops", () => {
    const win = windowLike();
    const { signals } = browserPageBridge(win);
    const views: string[] = [];
    const selected: (string | null)[] = [];
    const stopViews = signals.followViewChoice((v) => views.push(v));
    const stopSelection = signals.followSelection((id) => selected.push(id));
    win.dispatchEvent(new CustomEvent("whiteboard-set-view", { detail: { view: "whiteboard" } }));
    win.dispatchEvent(new CustomEvent("whiteboard-select", { detail: { session_id: "s9" } }));
    win.dispatchEvent(new CustomEvent("whiteboard-select", { detail: { session_id: "" } }));
    assert.deepEqual([views, selected], [["whiteboard"], ["s9", null]]);
    stopViews();
    stopSelection();
    win.dispatchEvent(new CustomEvent("whiteboard-set-view", { detail: { view: "list" } }));
    assert.deepEqual(views, ["whiteboard"]);
  });

  test("an event that does not read is ignored", () => {
    const win = windowLike();
    const { signals } = browserPageBridge(win);
    const views: string[] = [];
    signals.followViewChoice((v) => views.push(v));
    win.dispatchEvent(new CustomEvent("whiteboard-set-view", { detail: { view: "timeline" } }));
    win.dispatchEvent(new CustomEvent("whiteboard-set-view", { detail: null }));
    assert.deepEqual(views, []);
  });
});
