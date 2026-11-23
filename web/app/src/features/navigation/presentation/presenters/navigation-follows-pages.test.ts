import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { CurrentTopBar } from "../../application/current-top-bar";
import { followNavigation } from "../../application/follow-navigation";
import { TopBarSourceError } from "../../application/ports";
import { memoryNavigationSignal } from "../../infrastructure/memory-navigation-signal";
import { memoryPageOpener } from "../../infrastructure/memory-page-opener";
import { memoryTopBarSource } from "../../infrastructure/memory-top-bar-source";
import { linksWith, topBarJson, topBarFromJson } from "../../testing/fixtures";
import { PageLinksPresenter } from "./page-links-presenter";
import { ProjectPickerPresenter } from "./project-picker-presenter";

const page = (path: string, project: string, current?: string, attention = false) => topBarFromJson(topBarJson({ path, current_project_id: project, links: linksWith({ ...(current ? { [current]: { current: true } } : {}), ...(attention ? { agent_isolation: { attention: true } } : {}) }) }));

const setup = (initial = page("/", ""), options: { fresh?: boolean } = {}) => {
  const current = new CurrentTopBar(initial);
  const source = memoryTopBarSource((path) => topBarFromJson(topBarJson({ path })));
  const signal = memoryNavigationSignal();
  const stop = followNavigation(signal, source, current, options);
  return { current, source, signal, stop, picker: new ProjectPickerPresenter(current, memoryPageOpener()), links: new PageLinksPresenter(current) };
};
const settle = () => new Promise((resolve) => setImmediate(resolve));
const currentKeys = (links: PageLinksPresenter) => links.viewModel.links.filter((l) => l.current).map((l) => l.key);

describe("the top bar follows the page", () => {
  test("a navigation refetches and moves the current link and the current project", async () => {
    const { source, signal, picker, links } = setup();
    source.serve("/projects/p2", page("/projects/p2", "p2", "projects"));
    signal.emit("/projects/p2");
    await settle();
    assert.equal(picker.viewModel.selected, "p2");
    assert.deepEqual(currentKeys(links), ["projects"]);
    assert.deepEqual(source.asked, ["/projects/p2"]);
  });

  test("the later of two quick navigations wins even when its answer comes first", async () => {
    const { source, signal, picker, links } = setup();
    source.serve("/a", page("/a", "p1", "agents"));
    source.serve("/b", page("/b", "p2", "models"));
    const releaseA = source.hold("/a");
    signal.emit("/a");
    signal.emit("/b");
    await settle();
    releaseA();
    await settle();
    assert.equal(picker.viewModel.selected, "p2");
    assert.deepEqual(currentKeys(links), ["models"]);
  });

  test("a failed refetch keeps the last model, and the next navigation asks again", async () => {
    const { source, signal, picker } = setup(page("/", "p1"));
    source.failWith(new TopBarSourceError("NETWORK_ERROR", "offline"));
    signal.emit("/projects/p2");
    await settle();
    assert.equal(picker.viewModel.selected, "p1");
    source.heal();
    source.serve("/projects/p2", page("/projects/p2", "p2"));
    signal.emit("/projects/p2");
    await settle();
    assert.equal(picker.viewModel.selected, "p2");
  });

  test("a swap of <main> only, to the same path, asks again: the page's state can have changed", async () => {
    const { source, signal, links } = setup(page("/agents", "", "agents")); // a mount of the first page load
    source.serve("/agents", page("/agents", "", "agents", true));
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, ["/agents"]);
    assert.equal(links.viewModel.links.some((l) => l.attention), true);
  });

  test("a Back that arrives as a popstate and a navigation asks once", async () => {
    const { source, signal } = setup();
    signal.emit("/agents");
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, ["/agents"]);
  });

  test("a top bar that a swap inserted with fresh props skips the event of that swap, and no other", async () => {
    const { source, signal } = setup(page("/projects/p1", "p1"), { fresh: true });
    signal.emit("/projects/p1"); // the navigation that brought it
    await settle();
    assert.deepEqual(source.asked, []);
    signal.emit("/projects/p1"); // a later swap of <main> to the same path
    await settle();
    assert.deepEqual(source.asked, ["/projects/p1"]);
  });

  test("stops following when stopped", async () => {
    const { source, signal, stop } = setup();
    stop();
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, []);
  });
});
