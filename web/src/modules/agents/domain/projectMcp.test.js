import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog, toProjectMcpSettings } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import {
  addedNames, banWarning, changedServers, mergeSettings, namesInState, projectMcpRows, removedFromProject, resetServerOverrides, sessionExample,
  setNames, setServerState, setToolOverride, settingsChanged, settingsCount, settingsDraft, settingsPhrase, settingsToWrite, toolRows,
} from "./projectMcp.js";

file("agents/domain/projectMcp");

const server = (o) => ({ name: "s", transport: "stdio", command: "npx", args: [], env: {}, url: "", headers: {}, env_refs: [], agents: [], projects: [], problems: [], ...o });
const catalogWith = (servers, agents) => {
  const dto = catalogDTO();
  dto.mcp_servers = servers;
  if (agents) dto.agents = agents;
  return toCatalog(dto);
};
const agent = (name, mcp) => ({ name, description: "d", aliases: [], entries: [], resolved_skills: [], dropped_duplicates: [], missing_skills: [], mcp_servers: mcp, allowed_tools: [], problems: [] });
const tool = (name, def = "enabled", override = "default") => ({ name, default: def, override, effective: override === "default" ? def : override });
/** A project's read: one entry per [name, state, tools, usedBy]. */
const read = (...servers) => toProjectMcpSettings({
  mcp_servers: servers.filter(([, state]) => state === "added").map(([name]) => name),
  servers: servers.map(([name, state, tools = null, used = []]) => ({ name, state, used_by_agents: used, tools })),
});

test("every settings server is a row with its transport, variables, who else uses it and its three-state choice", () => {
  const c = catalogWith([
    server({ name: "github", transport: "http", url: "https://x", headers: { A: "Bearer ${GH}" }, env_refs: [{ name: "GH", set: true }], agents: ["a", "b"], projects: [{ id: "p1", name: "harness" }] }),
    server({ name: "pg", env: { U: "${DB}" }, env_refs: [{ name: "DB", set: false }], agents: ["a"], projects: [{ id: "p1", name: "harness" }, { id: "p2", name: "billing" }] }),
    server({ name: "browser" }),
  ]);
  const settings = read(["github", "added"], ["pg", "agent"], ["browser", "banned"]);
  const rows = projectMcpRows(c, "p1", settings, settingsDraft(settings));
  assert.deepEqual(rows.map((r) => [r.name, r.transport, r.state, r.stateLabel]), [["github", "http", "added", "Added"], ["pg", "stdio", "agent", "Agent’s choice"], ["browser", "stdio", "banned", "Banned"]]);
  assert.deepEqual(rows[1].choices.map((x) => [x.value, x.label, x.pressed]), [["added", "Added", false], ["agent", "Agent’s choice", true], ["banned", "Banned", false]]);
  assert.deepEqual(rows[0].vars.map((v) => [v.label, v.state, v.tone]), [["${GH}", "set", "success"]]);
  assert.equal(rows[0].who, "Used by 2 agents");
  assert.equal(rows[1].who, "Used by 1 agent and 1 other project");
  assert.ok(/fails to start/.test(rows[1].warning));
  assert.equal(rows[2].who, "Unused");
});

test("a server only another project uses says so", () => {
  const c = catalogWith([server({ name: "x", projects: [{ id: "p2", name: "billing" }] })]);
  const settings = read(["x", "agent"]);
  assert.equal(projectMcpRows(c, "p1", settings, settingsDraft(settings))[0].who, "Used by 1 other project");
});

test("a server the settings do not list gets no row, and one the catalog lacks gets none either", () => {
  const c = catalogWith([server({ name: "services", transport: "built_in", command: "", built_in: true }), server({ name: "a" })]);
  const settings = read(["a", "agent"], ["ghost", "agent"]);
  assert.deepEqual(projectMcpRows(c, "p1", settings, settingsDraft(settings)).map((r) => r.name), ["a"]);
});

test("no secret value leaves a row: plain env text is not in it", () => {
  const c = catalogWith([server({ name: "x", env: { TOKEN: "hunter2-secret" } })]);
  const settings = read(["x", "agent"]);
  assert.ok(!JSON.stringify(projectMcpRows(c, "p1", settings, settingsDraft(settings))).includes("hunter2"));
});

test("tool rows show the choice and what is in effect: the default unless the project overrides it", () => {
  const s = read(["gh", "agent", [tool("create_issue", "disabled"), tool("search", "enabled"), tool("list", "enabled", "disabled"), tool("close", "disabled", "enabled")]]);
  const v = toolRows(s.servers[0], settingsDraft(s).gh);
  assert.equal(v.unknown, false);
  assert.deepEqual(v.rows.map((r) => [r.name, r.choice, r.effective, r.source]), [
    ["create_issue", "default", "disabled", "from the default"],
    ["search", "default", "enabled", "from the default"],
    ["list", "disabled", "disabled", "set for this project"],
    ["close", "enabled", "enabled", "set for this project"],
  ]);
  assert.deepEqual(v.rows[0].choices.map((x) => [x.label, x.pressed]), [["Default (disabled)", true], ["Enabled", false], ["Disabled", false]]);
  assert.equal(v.summary, "4 tools, 2 disabled here");
});

test("a server that lists no tools says so", () => {
  const s = read(["gh", "agent", []]);
  assert.equal(toolRows(s.servers[0], settingsDraft(s).gh).summary, "The server lists no tools.");
});

test("tools never learned give a message and no rows", () => {
  const s = read(["gh", "agent"]);
  const v = toolRows(s.servers[0], settingsDraft(s).gh);
  assert.equal(v.unknown, true);
  assert.deepEqual(v.rows, []);
  assert.ok(/never probed, so every tool is enabled/.test(v.text));
  const c = catalogWith([server({ name: "gh" })]);
  const [row] = projectMcpRows(c, "p1", s, settingsDraft(s));
  assert.deepEqual([row.toolsUnknown, row.hasToolRows], [true, false]);
});

test("a banned server shows no tool list and says what the ban does; the overrides are kept", () => {
  const s = read(["gh", "banned", [tool("a", "enabled", "disabled")], ["designer", "backend"]]);
  const c = catalogWith([server({ name: "gh" })]);
  const [row] = projectMcpRows(c, "p1", s, settingsDraft(s));
  assert.deepEqual([row.showTools, row.hasToolRows, row.toolsUnknown, row.banned], [false, false, false, true]);
  assert.equal(row.banWarning, "Banned: no session in this project gets this server, whatever its agent lists. The 2 agents that list it, designer, backend, lose it in this project.");
  assert.deepEqual(settingsDraft(s).gh.overrides, { a: "disabled" });
});

test("the ban warning names the agents that list the server: none, one and many", () => {
  assert.ok(/No agent lists it, so no agent loses anything here\.$/.test(banWarning([])));
  assert.ok(/The agent that lists it, designer, loses it in this project\.$/.test(banWarning(["designer"])));
  assert.ok(/The 3 agents that list it, a, b, c, lose it in this project\.$/.test(banWarning(["a", "b", "c"])));
});

test("change detection compares states and overrides, not order or Default entries", () => {
  const s = read(["a", "agent", [tool("t1"), tool("t2")]], ["b", "added"]);
  let d = settingsDraft(s);
  assert.equal(settingsChanged(s, d), false);
  d = setServerState(d, "b", "banned");
  assert.deepEqual(changedServers(s, d), ["b"]);
  d = setServerState(d, "b", "added");
  assert.equal(settingsChanged(s, d), false);
  d = setToolOverride(d, "a", "t1", "disabled");
  assert.deepEqual(changedServers(s, d), ["a"]);
  d = setToolOverride(d, "a", "t1", "default");
  assert.equal(settingsChanged(s, d), false, "back to Default is no change");
});

test("Reset to defaults sets every override of a server to Default and touches no other server", () => {
  const s = read(["a", "agent", [tool("t1"), tool("t2")]], ["b", "agent", [tool("t1")]]);
  let d = settingsDraft(s);
  d = setToolOverride(setToolOverride(setToolOverride(d, "a", "t1", "disabled"), "a", "t2", "enabled"), "b", "t1", "disabled");
  d = resetServerOverrides(d, "a");
  assert.deepEqual(d.a.overrides, {});
  assert.deepEqual(d.b.overrides, { t1: "disabled" });
  const [rowA] = projectMcpRows(catalogWith([server({ name: "a" }), server({ name: "b" })]), "p1", s, d);
  assert.equal(rowA.canReset, false);
});

test("the write body holds every server's state and its non-default overrides", () => {
  const s = read(["a", "banned", [tool("t1"), tool("t2")]], ["b", "added"], ["c", "agent"]);
  let d = setToolOverride(settingsDraft(s), "a", "t2", "disabled");
  d = setServerState(d, "c", "added");
  assert.deepEqual(settingsToWrite(s, d), [
    { name: "a", state: "banned", toolOverrides: { t2: "disabled" } },
    { name: "b", state: "added", toolOverrides: {} },
    { name: "c", state: "added", toolOverrides: {} },
  ]);
});

test("a read again keeps the states and overrides the person changed, and takes the rest, and the new tool list", () => {
  const before = read(["a", "agent", [tool("t1")]], ["b", "agent"], ["gone", "agent"]);
  let d = setServerState(setToolOverride(settingsDraft(before), "a", "t1", "disabled"), "b", "banned");
  const after = read(["a", "agent", [tool("t1"), tool("t2")]], ["b", "agent"], ["fresh", "added"]);
  const merged = mergeSettings(before, d, after);
  assert.deepEqual(Object.keys(merged), ["a", "b", "fresh"], "a deleted server leaves, a new one comes");
  assert.deepEqual(merged.a.overrides, { t1: "disabled" });
  assert.equal(merged.b.state, "banned");
  assert.equal(merged.fresh.state, "added");
  const untouched = mergeSettings(before, settingsDraft(before), read(["a", "banned", [tool("t1")]], ["b", "agent"]));
  assert.equal(untouched.a.state, "banned", "what the person did not change follows the server");
});

test("the count and toast phrase say how the servers stand", () => {
  const s = read(["a", "added"], ["b", "agent"], ["c", "banned"], ["d", "banned"]);
  assert.equal(settingsCount(s, settingsDraft(s)), "4 servers: 1 Added, 1 Agent’s choice, 2 Banned");
  assert.equal(settingsPhrase(s), "gives 1 MCP server to its sessions and bans 2");
  assert.equal(settingsPhrase(read(["a", "added"], ["b", "added"])), "gives 2 MCP servers to its sessions");
  assert.deepEqual([addedNames(s), setNames(s), namesInState(s, settingsDraft(s), "banned")], [["a"], ["a", "c", "d"], ["c", "d"]]);
});

test("a server taken off the project when the catalog changed is named", () => {
  const c = catalogWith([server({ name: "a" })]);
  assert.deepEqual(removedFromProject(["a", "old-search", "b"], c), ["old-search", "b"]);
  assert.deepEqual(removedFromProject(["a"], c), []);
});

test("a rename rewrites the settings: no server is named as taken off", () => {
  const c = catalogWith([server({ name: "a" }), server({ name: "renamed" })]);
  assert.deepEqual(removedFromProject(["a", "old"], c, ["a", "renamed"]), []);
});

test("What a session gets: the agent's servers plus the project's Added ones, a shared name once, a banned server in neither", () => {
  const c = catalogWith([server({ name: "github" }), server({ name: "linear" }), server({ name: "figma" }), server({ name: "pg" })], [agent("ux", ["github", "figma", "pg"])]);
  const s = read(["github", "added"], ["linear", "added"], ["figma", "agent"], ["pg", "banned"]);
  const ex = sessionExample(c, s, settingsDraft(s));
  assert.equal(ex.agent, "ux");
  assert.deepEqual(ex.withAgent.map((x) => [x.name, x.origin]), [["github", "both, counted once"], ["figma", "agent"], ["linear", "project"]]);
  assert.deepEqual(ex.withoutAgent, ["github", "linear"]);
  assert.deepEqual(ex.banned, ["pg"]);
});

test("the example follows the draft, not what is saved", () => {
  const c = catalogWith([server({ name: "github" })], [agent("ux", ["github"])]);
  const s = read(["github", "agent"]);
  const ex = sessionExample(c, s, setServerState(settingsDraft(s), "github", "banned"));
  assert.deepEqual([ex.withAgent, ex.withoutAgent, ex.banned], [[], [], ["github"]]);
});

test("the example is the first agent that has servers, else the first agent, else none", () => {
  const two = catalogWith([], [agent("a", []), agent("b", ["x"])]);
  const none = read();
  assert.equal(sessionExample(two, none, {}).agent, "b");
  assert.equal(sessionExample(catalogWith([], [agent("a", [])]), none, {}).agent, "a");
  assert.equal(sessionExample(catalogWith([], []), none, {}).agent, "");
});
