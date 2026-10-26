import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import { DELETE_UNDO_NOTE, deleteBaseVersions, deleteImpact } from "./deleteImpact.js";

file("agents/domain/deleteImpact");

const match = (text, re) => assert.ok(re.test(text), `${text} should match ${re}`);

const catalog = () => {
  const dto = catalogDTO();
  dto.bundles.push({ name: "all", entries: ["@ux", "tdd"], resolved_skills: ["lint", "tdd"], used_by_agents: ["backend"], used_by_bundles: [] });
  dto.bundles[0].used_by_bundles = ["all"];
  dto.bundles[0].used_by_agents = ["ux-designer", "backend"];
  dto.skills[1].agents = [];
  dto.skills[1].bundles = ["ux"];
  return toCatalog(dto);
};

const names = (group) => group.items.map((i) => i.name);

test("a skill reaches the agents that list it, the bundles that hold it, and every agent through either", () => {
  const c = toCatalog(catalogDTO());
  const d = deleteImpact(c, "skill", "tdd");
  assert.equal(d.title, "Delete skill “tdd”?");
  assert.equal(d.typed, "tdd");
  assert.equal(d.button, "Delete skill");
  assert.deepEqual(d.groups.map((g) => [g.key, names(g)]), [["agents", ["ux-designer", "backend"]]]);
  assert.equal(d.groups[0].items[0].href, "/agents/ux-designer");
  match(d.total, /2 agents/);
  const lint = deleteImpact(c, "skill", "lint");
  assert.deepEqual(lint.groups.find((g) => g.key === "bundles").items.map((i) => i.name), ["@ux"]);
  assert.equal(lint.groups.find((g) => g.key === "bundles").items[0].href, "/agents/bundles/ux");
  match(lint.total, /1 agent\b/);
});

test("a bundle reaches the agents and bundles that name it, and says how many skills those agents can lose", () => {
  const d = deleteImpact(catalog(), "bundle", "ux");
  assert.equal(d.title, "Delete bundle “@ux”?");
  assert.equal(d.typed, "ux", "the person types the name, not the @ prefix");
  assert.deepEqual(d.groups.map((g) => [g.key, names(g)]), [["agents", ["ux-designer", "backend"]], ["bundles", ["@all"]]]);
  match(d.total, /up to 1 skill\b/);
  assert.equal(d.reachesNothing, false);
});

test("a bundle nothing names reaches nothing, and the dialog says so", () => {
  const dto = catalogDTO();
  dto.bundles[0].used_by_agents = [];
  const d = deleteImpact(toCatalog(dto), "bundle", "ux");
  assert.equal(d.reachesNothing, true);
  assert.equal(d.nothing, "No agent or bundle names it.");
  assert.equal(d.total, "");
});

test("an MCP server reaches the agents and projects that get it, the project linking to its settings", () => {
  const d = deleteImpact(toCatalog(catalogDTO()), "mcp", "github");
  assert.equal(d.title, "Delete MCP server “github”?");
  assert.equal(d.button, "Delete server");
  assert.deepEqual(d.groups.map((g) => [g.key, names(g)]), [["agents", ["ux-designer"]], ["projects", ["harness"]]]);
  assert.equal(d.groups[1].items[0].href, "/projects/p1/settings");
});

test("an agent lists no projects: the catalog read does not say where it is enabled", () => {
  const d = deleteImpact(toCatalog(catalogDTO()), "agent", "backend");
  assert.equal(d.title, "Delete agent “backend”?");
  assert.deepEqual(d.groups, []);
  match(d.intro, /skills, bundles and MCP servers stay/);
});

test("an item the catalog lacks has no dialog", () => {
  assert.equal(deleteImpact(toCatalog(catalogDTO()), "skill", "ghost"), null);
  assert.equal(deleteImpact(toCatalog(catalogDTO()), "tool", "Read"), null);
});

test("the dialog says the catalog is not in git, and never offers git as a backup", () => {
  match(DELETE_UNDO_NOTE, /Undo/);
  match(DELETE_UNDO_NOTE, /next catalog change or a server restart/);
  match(DELETE_UNDO_NOTE, /not in git/);
  match(DELETE_UNDO_NOTE, /no other copy/);
  const c = catalog();
  for (const [kind, name] of [["agent", "backend"], ["skill", "tdd"], ["bundle", "ux"], ["mcp", "github"]]) {
    const d = deleteImpact(c, kind, name);
    const text = JSON.stringify(d).toLowerCase();
    assert.ok(!/git (restore|checkout|history)|backup|commit|recover from git|in your repo/.test(text), `${kind}: ${text}`);
  }
  assert.ok(!/backup|restore|commit|checkout/i.test(DELETE_UNDO_NOTE));
});

test("a delete sends the versions of the files it changes, only those the catalog read has", () => {
  const c = toCatalog(catalogDTO());
  assert.deepEqual(deleteBaseVersions(c, "agent"), { "agents.json": "sha256:a1" });
  assert.deepEqual(deleteBaseVersions(c, "bundle"), { "agents.json": "sha256:a1" });
  assert.deepEqual(deleteBaseVersions(c, "skill"), { "agents.json": "sha256:a1" });
  assert.deepEqual(deleteBaseVersions(c, "mcp"), { "agents.json": "sha256:a1", "mcp-servers.json": "sha256:m1" });
  assert.deepEqual(deleteBaseVersions({ versions: {} }, "mcp"), {});
});
