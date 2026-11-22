// Gap tests for the picker, the links and the navigation follow, from the user
// stories of "Spec: Web UI — React top bar": through the use cases on the
// in-memory adapters, no React.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { CurrentTopBar } from "../../application/current-top-bar";
import { followNavigation } from "../../application/follow-navigation";
import { TopBarSourceError, type TopBarSource } from "../../application/ports";
import { parseTopBar, type TopBar } from "../../domain/top-bar";
import { browserNavigationSignal } from "../../infrastructure/browser-navigation-signal";
import { memoryNavigationSignal } from "../../infrastructure/memory-navigation-signal";
import { memoryPageOpener } from "../../infrastructure/memory-page-opener";
import { memoryTopBarSource } from "../../infrastructure/memory-top-bar-source";
import { linksWith, topBarJson, type TopBarJson } from "../../testing/fixtures";
import { PageLinksPresenter } from "./page-links-presenter";
import { ProjectPickerPresenter } from "./project-picker-presenter";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const model = (json: Partial<TopBarJson>): TopBar => parseTopBar(topBarJson(json));
const keys = (links: PageLinksPresenter, flag: "current" | "attention") => links.viewModel.links.filter((l) => l[flag]).map((l) => l.key);

function setup(initial: TopBar) {
  const current = new CurrentTopBar(initial);
  const opener = memoryPageOpener();
  return { current, opener, picker: new ProjectPickerPresenter(current, opener), links: new PageLinksPresenter(current) };
}

describe("project picker: gaps", () => {
  test("a page with no current project shows the placeholder, and moving to a project page and back brings it back", async () => {
    const current = new CurrentTopBar(model({ path: "/agents", current_project_id: "" }));
    const picker = new ProjectPickerPresenter(current, memoryPageOpener());
    const source = memoryTopBarSource((path) => model({ path }));
    source.serve("/projects/p2", model({ path: "/projects/p2", current_project_id: "p2" }));
    const signal = memoryNavigationSignal();
    followNavigation(signal, source, current);
    assert.deepEqual([picker.viewModel.showPlaceholder, picker.viewModel.selected, picker.viewModel.placeholder], [true, "", "Choose a project…"]);
    signal.emit("/projects");
    signal.emit("/projects/p2");
    await settle();
    assert.deepEqual([picker.viewModel.showPlaceholder, picker.viewModel.selected], [false, "p2"]);
    signal.emit("/agents");
    await settle();
    assert.deepEqual([picker.viewModel.showPlaceholder, picker.viewModel.selected], [true, ""]);
  });

  test("a project id that needs escaping reaches the switch href escaped", () => {
    const { picker, opener } = setup(model({ projects: [{ id: "a&b=c d", name: "Odd" }], current_project_id: "" }));
    picker.choose("a&b=c d");
    assert.deepEqual(opener.opened, ["/switch-project?project=a%26b%3Dc+d"]);
  });

  test("choosing the placeholder (the empty id) opens nothing", () => {
    const { picker, opener } = setup(model({ current_project_id: "" }));
    picker.choose("");
    assert.deepEqual(opener.opened, []);
  });

  test("choosing the project already current still opens it (the person may want its home page)", () => {
    const { picker, opener } = setup(model({ current_project_id: "p1" }));
    picker.choose("p1");
    assert.deepEqual(opener.opened, ["/switch-project?project=p1"]);
  });

  test("no projects and no current project: the picker is not offered", () => {
    const { picker } = setup(model({ projects: [], current_project_id: "" }));
    assert.equal(picker.viewModel.visible, false);
  });

  test("two names that differ by case are both listed, in the server's order", () => {
    const { picker } = setup(model({ projects: [{ id: "a", name: "alpha" }, { id: "b", name: "Alpha" }] }));
    assert.deepEqual(picker.viewModel.options.map((o) => o.id), ["a", "b"]);
  });
});

describe("page links: gaps", () => {
  test("the attention mark on another key shows there and nowhere else, and it moves with the page", async () => {
    const attentionOnServer = model({ path: "/", links: linksWith({ server: { attention: true } }) });
    const { current, links } = setup(attentionOnServer);
    assert.deepEqual(keys(links, "attention"), ["server"]);
    current.set(model({ path: "/models", links: linksWith({ models: { current: true }, agent_isolation: { attention: true } }) }));
    assert.deepEqual([keys(links, "attention"), keys(links, "current")], [["agent_isolation"], ["models"]]);
  });

  test("a link that is current and has attention carries both marks", () => {
    const { links } = setup(model({ links: linksWith({ agent_isolation: { current: true, attention: true } }) }));
    const link = links.viewModel.links.find((l) => l.key === "agent_isolation")!;
    assert.deepEqual([link.current, link.attention, link.label, link.icon], [true, true, "Agent isolation", "shield"]);
  });

  test("unknown keys are dropped but the links around them keep their order, even with attention on one of the unknown", () => {
    const { links } = setup(
      model({
        links: [
          { key: "board", href: "/", current: false, attention: false },
          { key: "from_the_future", href: "/f", current: true, attention: true },
          { key: "server", href: "/server", current: false, attention: true },
        ],
      }),
    );
    assert.deepEqual(links.viewModel.links.map((l) => l.key), ["board", "server"]);
    assert.deepEqual([keys(links, "current"), keys(links, "attention")], [[], ["server"]]);
  });

  test("with every served link gone, the bar has no links (nothing is invented)", () => {
    const { links } = setup(model({ links: [] }));
    assert.deepEqual(links.viewModel.links, []);
  });

  test("a project page with New project in place of Projects", () => {
    const { links } = setup(model({ links: [{ key: "board", href: "/", current: true, attention: false }, { key: "new_project", href: "/projects/new", current: false, attention: false }] }));
    assert.deepEqual(links.viewModel.links.map((l) => l.label), ["Agent board", "New project"]);
  });
});

describe("the top bar follows the page: races and signals", () => {
  const rig = (initial: TopBar, options: { fresh?: boolean } = {}) => {
    const current = new CurrentTopBar(initial);
    const source = memoryTopBarSource((path) => model({ path }));
    const signal = memoryNavigationSignal();
    followNavigation(signal, source, current, options);
    return { current, source, signal };
  };

  test("a failed answer for the page the person is on does not let a late answer for the page they left take its place", async () => {
    const answers = new Map<string, (ok: boolean) => void>();
    const source: TopBarSource = {
      // Ignores the abort, as a source behind a slow network might.
      load: (path) =>
        new Promise((resolve, reject) => {
          answers.set(path, (ok) => (ok ? resolve(model({ path, current_project_id: "p1" })) : reject(new TopBarSourceError("NETWORK_ERROR", "offline"))));
        }),
    };
    const current = new CurrentTopBar(model({ path: "/" }));
    const signal = memoryNavigationSignal();
    followNavigation(signal, source, current);
    signal.emit("/a");
    signal.emit("/b");
    answers.get("/b")!(false);
    await settle();
    answers.get("/a")!(true);
    await settle();
    assert.equal(current.get().path, "/", "the page the person left must not be shown");
  });

  test("three navigations answered in the reverse order show the last one", async () => {
    const { current, source, signal } = rig(model({ path: "/" }));
    const releases = ["/a", "/b", "/c"].map((p) => source.hold(p));
    for (const p of ["/a", "/b", "/c"]) signal.emit(p);
    await settle();
    for (const release of releases.reverse()) {
      release();
      await settle();
    }
    assert.equal(current.get().path, "/c");
  });

  test("going to a page and back to the one the bar was built for asks for it, and shows it", async () => {
    const { current, source, signal } = rig(model({ path: "/agents", links: linksWith({ agents: { current: true } }) }), { fresh: true });
    signal.emit("/agents"); // the swap that brought these props
    signal.emit("/models");
    await settle();
    source.serve("/agents", model({ path: "/agents", links: linksWith({ agents: { current: true }, agent_isolation: { attention: true } }) }));
    signal.emit("/agents");
    await settle();
    assert.deepEqual(source.asked, ["/models", "/agents"]);
    assert.equal(current.get().links.find((l) => l.key === "agent_isolation")!.attention, true);
  });

  test("a navigation for the page already being asked for does not cancel that ask or answer twice", async () => {
    const { current, source, signal } = rig(model({ path: "/" }));
    const release = source.hold("/a");
    signal.emit("/a");
    signal.emit("/a");
    await settle();
    release();
    await settle();
    assert.deepEqual(source.asked, ["/a"]);
    assert.equal(current.get().path, "/a");
  });

  test("a malformed answer keeps the last model and does not throw out of the listener", async () => {
    const source: TopBarSource = { load: async () => parseTopBar({ path: 3 }) };
    const current = new CurrentTopBar(model({ path: "/" }));
    const signal = memoryNavigationSignal();
    followNavigation(signal, source, current);
    signal.emit("/a");
    await settle();
    assert.equal(current.get().path, "/");
  });

  test("popstate then harness:navigated for one Back, on the browser's events, asks once and lands on the entry's path", async () => {
    const listeners = new Map<string, Set<(event: Event) => void>>();
    const win = {
      location: { pathname: "/" },
      addEventListener: (type: string, fn: (event: Event) => void) => void (listeners.get(type) ?? listeners.set(type, new Set()).get(type)!).add(fn),
      removeEventListener: (type: string, fn: (event: Event) => void) => void listeners.get(type)?.delete(fn),
    };
    const fire = (type: string, detail?: unknown) => listeners.get(type)?.forEach((fn) => fn(Object.assign(new Event(type), detail === undefined ? {} : { detail })));
    const current = new CurrentTopBar(model({ path: "/projects/p1" }));
    const source = memoryTopBarSource((path) => model({ path }));
    followNavigation(browserNavigationSignal(win as never), source, current);
    win.location.pathname = "/agents";
    fire("popstate");
    fire("harness:navigated", { path: "/agents" });
    await settle();
    assert.deepEqual(source.asked, ["/agents"]);
    assert.equal(current.get().path, "/agents");
  });

  test("a harness:navigated with a path that is not a string is ignored", async () => {
    const listeners: Array<(event: Event) => void> = [];
    const win = { location: { pathname: "/" }, addEventListener: (type: string, fn: (event: Event) => void) => void (type === "harness:navigated" && listeners.push(fn)), removeEventListener: () => {} };
    const source = memoryTopBarSource((path) => model({ path }));
    followNavigation(browserNavigationSignal(win as never), source, new CurrentTopBar(model({ path: "/" })));
    for (const fn of listeners) fn(Object.assign(new Event("harness:navigated"), { detail: { path: 42 } }));
    await settle();
    assert.deepEqual(source.asked, []);
  });
});
