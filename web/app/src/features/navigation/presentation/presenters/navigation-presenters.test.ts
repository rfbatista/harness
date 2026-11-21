import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { CurrentTopBar } from "../../application/current-top-bar";
import { parseTopBar } from "../../domain/top-bar";
import { memoryPageOpener } from "../../infrastructure/memory-page-opener";
import { linksWith, topBarJson, type TopBarJson } from "../../testing/fixtures";
import { PageLinksPresenter } from "./page-links-presenter";
import { ProjectPickerPresenter } from "./project-picker-presenter";

const setup = (json: Partial<TopBarJson> = {}) => {
  const current = new CurrentTopBar(parseTopBar(topBarJson(json)));
  const opener = memoryPageOpener();
  return { current, opener, picker: new ProjectPickerPresenter(current, opener), links: new PageLinksPresenter(current) };
};

describe("project picker", () => {
  test("lists every project by name with the current one selected", () => {
    const { picker } = setup({ current_project_id: "p2" });
    assert.deepEqual(picker.viewModel.options, [
      { id: "p1", name: "harness" },
      { id: "p2", name: "Website" },
    ]);
    assert.equal(picker.viewModel.selected, "p2");
    assert.equal(picker.viewModel.showPlaceholder, false);
    assert.equal(picker.viewModel.visible, true);
  });

  test("shows “Choose a project…” on a page with no current project", () => {
    const { picker } = setup({ current_project_id: "" });
    assert.equal(picker.viewModel.showPlaceholder, true);
    assert.equal(picker.viewModel.placeholder, "Choose a project…");
    assert.equal(picker.viewModel.selected, "");
  });

  test("a current project that is not listed counts as none", () => {
    assert.equal(setup({ current_project_id: "gone" }).picker.viewModel.showPlaceholder, true);
  });

  test("is not offered without projects", () => {
    assert.equal(setup({ projects: [] }).picker.viewModel.visible, false);
  });

  test("is a GET form on the switch href, so it works without JavaScript", () => {
    assert.equal(setup({ switch_project_href: "/switch" }).picker.viewModel.action, "/switch");
  });

  test("picking a project opens it at once, once", () => {
    const { picker, opener } = setup({ current_project_id: "p1" });
    picker.choose("p2");
    assert.deepEqual(opener.opened, ["/switch-project?project=p2"]);
  });

  test("picking a project that is not listed opens nothing", () => {
    const { picker, opener } = setup();
    picker.choose("nope");
    assert.deepEqual(opener.opened, []);
  });
});

describe("page links", () => {
  test("show only the served links, in the server's order, hrefs untouched", () => {
    const { links } = setup({ links: [{ key: "server", href: "/s?x=1", current: false, attention: false }, { key: "board", href: "/b", current: false, attention: false }] });
    assert.deepEqual(links.viewModel.links.map((l) => [l.key, l.href]), [["server", "/s?x=1"], ["board", "/b"]]);
  });

  test("name each link by its key, and give icon links a label to name and tooltip them", () => {
    const { links } = setup();
    const byKey = Object.fromEntries(links.viewModel.links.map((l) => [l.key, l]));
    assert.equal(byKey.board?.label, "Agent board");
    assert.equal(byKey.board?.icon, undefined);
    assert.equal(byKey.credentials?.label, "Git credentials");
    assert.equal(byKey.credentials?.icon, "key");
    assert.equal(byKey.token_savers?.icon, "saver");
    assert.equal(byKey.models?.icon, "model");
    assert.equal(byKey.agent_isolation?.label, "Agent isolation");
    assert.equal(byKey.agent_isolation?.icon, "shield");
    assert.equal(byKey.server?.icon, "server");
    assert.equal(byKey.agents?.note, "Agents are shared by every project");
  });

  test("New project stands where the projects list is not served", () => {
    const { links } = setup({ links: [{ key: "new_project", href: "/projects/new", current: false, attention: false }] });
    assert.deepEqual(links.viewModel.links.map((l) => l.label), ["New project"]);
  });

  test("mark the current page", () => {
    const { links } = setup({ links: linksWith({ agents: { current: true } }) });
    assert.deepEqual(links.viewModel.links.filter((l) => l.current).map((l) => l.key), ["agents"]);
  });

  test("carry the attention mark on any key that has it", () => {
    const { links } = setup({ links: linksWith({ agent_isolation: { attention: true }, models: { attention: true } }) });
    assert.deepEqual(links.viewModel.links.filter((l) => l.attention).map((l) => l.key), ["models", "agent_isolation"]);
  });

  test("ignore a link whose key is unknown, and fields it does not know", () => {
    const { links } = setup({ future: true, links: [{ key: "mystery", href: "/m", current: false, attention: false }, ...linksWith({}).slice(0, 1)] });
    assert.deepEqual(links.viewModel.links.map((l) => l.key), ["board"]);
  });
});
