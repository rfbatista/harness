import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { CurrentTopBar } from "../../application/current-top-bar";
import { followNavigation } from "../../application/follow-navigation";
import { TopBarSourceError } from "../../application/ports";
import { parseTopBar } from "../../domain/top-bar";
import { memoryNavigationSignal } from "../../infrastructure/memory-navigation-signal";
import { memoryPageOpener } from "../../infrastructure/memory-page-opener";
import { memoryTopBarSource } from "../../infrastructure/memory-top-bar-source";
import { linksWith, topBarJson } from "../../testing/fixtures";
import { PageLinksPresenter } from "./page-links-presenter";
import { ProjectPickerPresenter } from "./project-picker-presenter";

const page = (path: string, project: string, current?: string) => parseTopBar(topBarJson({ path, current_project_id: project, links: linksWith(current ? { [current]: { current: true } } : {}) }));

const setup = (initial = page("/", "")) => {
  const current = new CurrentTopBar(initial);
  const source = memoryTopBarSource((path) => parseTopBar(topBarJson({ path })));
  const signal = memoryNavigationSignal();
  const stop = followNavigation(signal, source, current);
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

  test("the page the top bar already shows is not asked for again", async () => {
    const { source, signal } = setup(page("/agents", "", "agents"));
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, []);
  });

  test("a Back that arrives as a popstate and a navigation asks once", async () => {
    const { source, signal } = setup();
    signal.emit("/agents");
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, ["/agents"]);
  });

  test("a top bar that was swapped in with fresh props asks for nothing", async () => {
    const { source, signal } = setup(page("/projects/p1", "p1"));
    signal.emit("/projects/p1");
    await settle();
    assert.deepEqual(source.asked, []);
  });

  test("stops following when stopped", async () => {
    const { source, signal, stop } = setup();
    stop();
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, []);
  });
});
