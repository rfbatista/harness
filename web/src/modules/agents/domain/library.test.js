import { assert, file, test } from "../../../shared/testing/test.js";
import { agentDetail, agentRows, agentsFooter, mcpTarget, problemBadge, startMenu, usageLine } from "./library.js";

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
  mcpServers: [{ name: "github", transport: "http", url: "https://api.example.com/mcp/", envRefs: [{ name: "GITHUB_TOKEN", set: false }, { name: "HOME", set: true }] }],
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
    name: "github", href: "/agents/mcp/github", found: true, transport: "http", target: "https://api.example.com/mcp/",
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

test("a server's target is its URL when remote and its command line when local; ${VAR} stays as written", () => {
  assert.equal(mcpTarget({ transport: "http", url: "https://mcp.example.com/mcp", command: "", args: [] }), "https://mcp.example.com/mcp");
  assert.equal(mcpTarget({ transport: "sse", url: "https://mcp.example.com/sse?key=${KEY}", args: [] }), "https://mcp.example.com/sse?key=${KEY}");
  assert.equal(mcpTarget({ transport: "stdio", command: "npx", args: ["-y", "@playwright/mcp", "--token=${TOKEN}"], url: "" }), "npx -y @playwright/mcp --token=${TOKEN}");
  assert.equal(mcpTarget({ transport: "stdio", command: "uv", args: [] }), "uv");
  assert.equal(mcpTarget({ transport: "stdio" }), "", "a server read without its fields shows no target");
});

test("a plain token in the URL or an argument is masked; the env and headers are never part of the target", () => {
  const token = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2";
  assert.equal(mcpTarget({ transport: "stdio", command: "gh-mcp", args: ["--auth", `--token=${token}`, token] }), "gh-mcp --auth --token=•••••• ••••••");
  assert.equal(mcpTarget({ transport: "http", url: `https://mcp.example.com/mcp?api_key=${token}&v=2` }), "https://mcp.example.com/mcp?api_key=••••••&v=2");
  assert.equal(mcpTarget({ transport: "http", url: "https://me:hunter2@mcp.example.com/mcp" }), "https://me:••••••@mcp.example.com/mcp");
  const t = mcpTarget({ transport: "http", url: "https://x/mcp", env: { A: token }, headers: { Authorization: `Bearer ${token}` } });
  assert.equal(t.includes(token), false);
});

test("each server row carries its transport and target, and a server missing from the catalog stays marked", () => {
  const c = { ...catalog, mcpServers: [{ name: "github", transport: "stdio", command: "npx", args: ["-y", "gh"], envRefs: [] }] };
  const [github, gone] = agentDetail(c, "ux-designer").servers;
  assert.deepEqual([github.transport, github.target, github.found], ["stdio", "npx -y gh", true]);
  assert.deepEqual([gone.transport, gone.target, gone.found], ["", "", false]);
});

test("the usage line counts sessions and zones, singular and plural, and is absent while the server sends no usage", () => {
  assert.equal(usageLine({ sessions: 31, zones: 2 }), "Used by 31 sessions · 2 zones");
  assert.equal(usageLine({ sessions: 1, zones: 1 }), "Used by 1 session · 1 zone");
  assert.equal(usageLine({ sessions: 0, zones: 0 }), "Used by 0 sessions · 0 zones");
  assert.equal(usageLine(null), "");
  assert.equal(usageLine(undefined), "");
  const withUsage = { ...catalog, agents: catalog.agents.map((a) => (a.name === "docs" ? { ...a, usage: { sessions: 2, zones: 0 } } : a)) };
  assert.equal(agentDetail(withUsage, "docs").usage, "Used by 2 sessions · 0 zones");
  assert.equal(agentDetail(withUsage, "backend").usage, "", "no usage field: no line");
});

test("Start a session as: the menu lists the projects that enable the agent, each linking to its sessions page with the agent", () => {
  const enabledIn = [{ id: "p2", name: "atlas-api", mode: "selected" }, { id: "p 1", name: "harness", mode: "all" }];
  const c = { ...catalog, agents: catalog.agents.map((a) => (a.name === "docs" ? { ...a, enabledIn } : a)) };
  const s = agentDetail(c, "docs", [{ id: "p 1", name: "harness" }, { id: "p2", name: "atlas-api" }]).start;
  assert.equal(s.label, "Start a session as docs");
  assert.equal(s.disabled, false);
  assert.deepEqual(s.projects.map((p) => [p.name, p.href, p.note]), [
    ["atlas-api", "/projects/p2/sessions?agent=docs", "Selected agents"],
    ["harness", "/projects/p%201/sessions?agent=docs", "All agents"],
  ]);
  assert.equal(s.reason, "");
});

test("Start a session as: an agent enabled nowhere is disabled, with the reason and the way to enable it", () => {
  const s = startMenu({ name: "docs", enabledIn: [] }, 2);
  assert.deepEqual([s.disabled, s.reason, s.projects], [true, "Not enabled in any project.", []]);
  assert.equal(s.enableHref, "/agents/docs/edit#projects");
  assert.equal(startMenu({ name: "docs", enabledIn: [] }, 0).reason, "There are no projects yet.");
  assert.equal(startMenu({ name: "a b", enabledIn: [{ id: "p", name: "n", mode: "all" }] }, 1).projects[0].href, "/projects/p/sessions?agent=a%20b");
});
