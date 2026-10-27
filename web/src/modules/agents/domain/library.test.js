import { assert, file, test } from "../../../shared/testing/test.js";
import { agentDetail, agentRows, agentsFooter, problemBadge } from "./library.js";

file("agents/domain/library");

const agent = (over) => ({
  name: "a", description: "d", aliases: [], entries: [], resolvedSkills: [], droppedDuplicates: [], missingSkills: [],
  mcpServers: [], allowedTools: [], problems: [], ...over,
});

// Worked example: ux holds @laws and lint; laws holds two skills.
const catalog = {
  root: "/data/agents",
  agents: [
    agent({
      name: "ux-designer", aliases: ["ux"], entries: ["@ux", "tdd", "ghost", "lint"],
      resolvedSkills: ["hicks-law", "fitts-law", "lint", "tdd", "ghost"], droppedDuplicates: ["lint"], missingSkills: ["ghost"],
      mcpServers: ["github", "gone"], allowedTools: ["Read"],
      problems: [{ code: "MISSING_SKILL", message: "skill ghost does not exist" }],
    }),
    agent({ name: "backend", description: "", entries: ["tdd"], resolvedSkills: ["tdd"], problems: [{ code: "NO_DESCRIPTION", message: "no description" }] }),
    agent({ name: "docs", description: "Writes docs.", entries: ["lint"], resolvedSkills: ["lint"] }),
  ],
  bundles: [
    { name: "ux", entries: ["@laws", "lint"], resolvedSkills: ["hicks-law", "fitts-law", "lint"], usedByAgents: ["ux-designer"], usedByBundles: [] },
    { name: "laws", entries: ["hicks-law", "fitts-law"], resolvedSkills: ["hicks-law", "fitts-law"], usedByAgents: [], usedByBundles: ["ux"] },
  ],
  skills: ["tdd", "lint", "hicks-law", "fitts-law"].map((name) => ({ name })),
  mcpServers: [{ name: "github", transport: "http", envRefs: [{ name: "GITHUB_TOKEN", set: false }, { name: "HOME", set: true }] }],
};

test("a missing description and a missing skill each show as a badge that replaces the meta line", () => {
  assert.deepEqual(problemBadge([{ code: "NO_DESCRIPTION", message: "" }]), { label: "No description", tone: "attention" });
  assert.deepEqual(problemBadge([{ code: "MISSING_SKILL", message: "" }, { code: "MISSING_SKILL", message: "" }]), { label: "2 missing skills", tone: "danger" });
  assert.deepEqual(problemBadge([{ code: "MISSING_SKILL", message: "" }]), { label: "1 missing skill", tone: "danger" });
  assert.deepEqual(problemBadge([{ code: "SOMETHING_NEW", message: "odd" }]), { label: "odd", tone: "danger" });
  assert.equal(problemBadge([]), null);
});

test("agent rows: the filter matches name, alias and skills; quick filters narrow; a broken row shows its badge", () => {
  const names = (o) => agentRows(catalog, o).map((r) => r.name);
  assert.deepEqual(names({}), ["ux-designer", "backend", "docs"]);
  assert.deepEqual(names({ query: "UX" }), ["ux-designer"], "by alias, any case");
  assert.deepEqual(names({ query: "fitts" }), ["ux-designer"], "by a skill reached through a bundle");
  assert.deepEqual(names({ filter: "mcp" }), ["ux-designer"]);
  assert.deepEqual(names({ filter: "problems" }), ["ux-designer", "backend"]);
  assert.deepEqual(names({ filter: "problems", query: "back" }), ["backend"]);
  const [ux, backend, docs] = agentRows(catalog, {});
  assert.equal(ux.badge.label, "1 missing skill");
  assert.equal(ux.meta, "5 skills · 2 MCP");
  assert.equal(backend.badge.label, "No description");
  assert.equal(backend.description, "No description");
  assert.equal(backend.dim, true);
  assert.equal(docs.badge, null);
  assert.equal(docs.meta, "1 skill");
  assert.equal(docs.href, "/agents/docs");
});

test("the footer names the source and counts agents and bundles", () => {
  assert.equal(agentsFooter(catalog), "agents.json · 3 agents, 2 bundles");
  assert.equal(agentsFooter({ agents: [agent({})], bundles: [{}] }), "agents.json · 1 agent, 1 bundle");
});

test("the detail expands bundles (nested ones included), keeps single skills, and totals what the server resolved", () => {
  const d = agentDetail(catalog, "ux-designer");
  assert.equal(d.skillsHeading, "Skills · 5 after bundles");
  assert.equal(d.groups.length, 1);
  const g = d.groups[0];
  assert.deepEqual([g.name, g.size, g.sizeLabel, g.href], ["@ux", 3, "3 skills", "/agents/bundles/ux"]);
  assert.deepEqual(g.entries.map((e) => e.label), ["@laws (2)", "lint"]);
  assert.deepEqual(g.entries.map((e) => e.href), ["/agents/bundles/laws", "/agents/skills/lint"]);
  assert.deepEqual(d.singles.map((s) => [s.name, s.missing]), [["tdd", false], ["ghost", true], ["lint", false]]);
  assert.equal(d.droppedNote, "1 duplicate dropped: lint");
});

test("the detail names MCP servers with transport and what is set, tools, and the Change links", () => {
  const d = agentDetail(catalog, "ux-designer");
  assert.equal(d.mcpHeading, "MCP servers · 2");
  assert.deepEqual(d.servers[0], {
    name: "github", href: "/agents/mcp/github", found: true, transport: "http",
    refs: [{ label: "${GITHUB_TOKEN}", set: false, state: "not set", tone: "attention" }, { label: "${HOME}", set: true, state: "set", tone: "success" }],
  });
  assert.deepEqual([d.servers[1].name, d.servers[1].found], ["gone", false]);
  assert.deepEqual(d.tools, ["Read"]);
  assert.equal(d.toolsDefault, false);
  assert.deepEqual(d.change, { identity: "/agents/ux-designer/edit#identity", skills: "/agents/ux-designer/edit#skills", mcp: "/agents/ux-designer/edit#mcp", tools: "/agents/ux-designer/edit#tools", projects: "/agents/ux-designer/edit#projects" });
  assert.equal(d.editHref, "/agents/ux-designer/edit");
  assert.equal(d.duplicateHref, "/agents/new?from=ux-designer");
  assert.equal(d.path, "agents.json › agents.ux-designer");
  assert.equal(d.copyPath, "/data/agents/agents.json");
});

test("an agent with no description, no servers and no tools says so", () => {
  const d = agentDetail(catalog, "backend");
  assert.equal(d.noDescription, true);
  assert.equal(d.badge.label, "No description");
  assert.equal(d.toolsDefault, true);
  assert.equal(d.noServers, true);
  assert.equal(d.skillsHeading, "Skills · 1 after bundles");
  assert.deepEqual(d.groups, []);
});

test("a bundle the agent names that does not exist is shown as missing; an unknown agent has no detail", () => {
  const c = { ...catalog, agents: [agent({ name: "x", entries: ["@gone"], resolvedSkills: [] })] };
  const g = agentDetail(c, "x").groups[0];
  assert.deepEqual([g.name, g.missing, g.size], ["@gone", true, 0]);
  assert.equal(agentDetail(catalog, "nobody"), null);
});

test("the detail carries Enabled in: the agent's projects against every project, and the Change link to the editor's Projects section", () => {
  const withProjects = { ...catalog, agents: catalog.agents.map((a) => (a.name === "ux-designer" ? { ...a, enabledIn: [{ id: "p1", name: "harness", mode: "all" }] } : a)) };
  const projects = [{ id: "p1", name: "harness" }, { id: "p2", name: "atlas-api" }];
  const d = agentDetail(withProjects, "ux-designer", projects);
  assert.equal(d.enabled.heading, "Enabled in · 1 of 2 projects");
  assert.deepEqual(d.enabled.notListed.map((p) => p.name), ["atlas-api"]);
  assert.equal(d.change.projects, "/agents/ux-designer/edit#projects");
  assert.equal(agentDetail(withProjects, "docs", projects).enabled.nowhere, true, "an agent with no enabled_in is enabled nowhere");
});
