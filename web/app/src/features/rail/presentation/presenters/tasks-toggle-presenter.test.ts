import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Rail } from "../../application/rail";
import type { ProjectNav } from "../../domain/nav";
import { manualScheduler, memoryAnnouncer, memoryFeaturePrefs, memoryPageOpener, memoryRailPrefs } from "../../infrastructure/memory-ports";
import { memoryRailFeed, memoryRailSource } from "../../infrastructure/memory-rail";
import { session, task } from "../../testing/fixtures";
import { RailPresenter } from "./rail-presenter";
import { TasksTogglePresenter } from "./tasks-toggle-presenter";

const NAV: ProjectNav = { newTaskHref: "/n", boardHref: "/b", codeHref: "", servicesHref: "", servicesProjectId: "p1", reviewsHref: "", documentsHref: "", designHref: "", designSystemHref: "", designSystemProposals: 0, usageHref: "", productOwnerHref: "" };

function rail() {
  const tasks = [task(), task({ id: "t2", status: "todo" })];
  const feed = memoryRailFeed();
  const features = memoryFeaturePrefs();
  const model = new Rail({ source: memoryRailSource({ tasks }), feed, seed: { projectId: "p1", tasks, sessions: [], archivedCount: 0 }, currentTaskId: "", collapsed: [], prefs: memoryRailPrefs(), features, announcer: memoryAnnouncer(), opener: memoryPageOpener(), scheduler: manualScheduler() });
  return { feed, presenter: new RailPresenter(model, features, { projectName: "harness", nav: NAV }) };
}

describe("the Tasks button", () => {
  test("it shows the server's count until the rail takes over, then the rail's, live", () => {
    const { feed, presenter } = rail();
    const toggle = new TasksTogglePresenter(3);
    assert.deepEqual([toggle.viewModel.needsYouCount, toggle.viewModel.hasNeedsYou], [3, true]);
    toggle.follow(presenter);
    assert.deepEqual([toggle.viewModel.needsYouCount, toggle.viewModel.hasNeedsYou], [0, false]);
    feed.emit({ kind: "upsert", session: session("a", "t2", "idle") });
    assert.deepEqual([toggle.viewModel.needsYouCount, toggle.viewModel.hasNeedsYou], [1, true]);
  });

  test("it says whether the drawer is open, and stops following", () => {
    const { presenter } = rail();
    const toggle = new TasksTogglePresenter(0);
    toggle.follow(presenter);
    presenter.openDrawer();
    assert.equal(toggle.viewModel.expanded, true);
    presenter.closeDrawer();
    assert.equal(toggle.viewModel.expanded, false);
    toggle.stop();
    presenter.openDrawer();
    assert.equal(toggle.viewModel.expanded, false);
  });
});
