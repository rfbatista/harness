import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import {
  projectMcpRows, projectMcpSaved, removedFromProject, selectionChanged, sessionExample,
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

test("every catalog server is a row with its transport, its variables and who else uses it", () => {
  const c = catalogWith([
    server({ name: "github", transport: "http", url: "https://x", headers: { A: "Bearer ${GH}" }, env_refs: [{ name: "GH", set: true }], agents: ["a", "b"], projects: [{ id: "p1", name: "harness" }] }),
    server({ name: "pg", env: { U: "${DB}" }, env_refs: [{ name: "DB", set: false }], agents: ["a"], projects: [{ id: "p1", name: "harness" }, { id: "p2", name: "billing" }] }),
    server({ name: "browser" }),
  ]);
  const rows = projectMcpRows(c, "p1", ["github"]);
  assert.deepEqual(rows.map((r) => [r.name, r.transport, r.checked]), [["github", "http", true], ["pg", "stdio", false], ["browser", "stdio", false]]);
  assert.deepEqual(rows[0].vars.map((v) => [v.label, v.state, v.tone]), [["${GH}", "set", "success"]]);
  assert.equal(rows[0].who, "Used by 2 agents");
  assert.equal(rows[1].who, "Used by 1 agent and 1 other project");
  assert.ok(/fails to start/.test(rows[1].warning));
  assert.equal(rows[0].warning, "");
  assert.equal(rows[2].who, "Unused");
  assert.deepEqual(rows[2].vars, []);
});

test("a server only another project uses says so", () => {
  const c = catalogWith([server({ name: "x", projects: [{ id: "p2", name: "billing" }] })]);
  assert.equal(projectMcpRows(c, "p1", [])[0].who, "Used by 1 other project");
});

test("no secret value leaves a row: plain env text is not in it", () => {
  const c = catalogWith([server({ name: "x", env: { TOKEN: "hunter2-secret" } })]);
  assert.ok(!JSON.stringify(projectMcpRows(c, "p1", [])).includes("hunter2"));
});

test("the selection changed when its set differs from the saved one, in any order", () => {
  assert.equal(selectionChanged(["a", "b"], ["b", "a"]), false);
  assert.equal(selectionChanged(["a"], ["a", "b"]), true);
  assert.equal(selectionChanged([], []), false);
});

test("the saved list keeps only names the catalog has, in catalog order", () => {
  const c = catalogWith([server({ name: "a" }), server({ name: "b" })]);
  assert.deepEqual(projectMcpSaved(c, ["b", "ghost", "a"]), ["a", "b"]);
});

test("a server taken off the project when the catalog changed is named", () => {
  const c = catalogWith([server({ name: "a" })]);
  assert.deepEqual(removedFromProject(["a", "old-search", "b"], c), ["old-search", "b"]);
  assert.deepEqual(removedFromProject(["a"], c), []);
});

test("a rename rewrites the list: no server is named as taken off", () => {
  const c = catalogWith([server({ name: "a" }), server({ name: "renamed" })]);
  assert.deepEqual(removedFromProject(["a", "old"], c, ["a", "renamed"]), []);
});

test("What a session gets: the agent's servers plus the project's, a shared name once, and no agent gets only the project's", () => {
  const c = catalogWith([server({ name: "github" }), server({ name: "linear" }), server({ name: "figma" })], [agent("ux", ["github", "figma"])]);
  const ex = sessionExample(c, ["github", "linear"]);
  assert.equal(ex.agent, "ux");
  assert.deepEqual(ex.withAgent.map((x) => [x.name, x.origin]), [["github", "both, counted once"], ["figma", "agent"], ["linear", "project"]]);
  assert.deepEqual(ex.withoutAgent, ["github", "linear"]);
});

test("the example is the first agent that has servers, else the first agent, else none", () => {
  const two = catalogWith([], [agent("a", []), agent("b", ["x"])]);
  assert.equal(sessionExample(two, []).agent, "b");
  assert.equal(sessionExample(catalogWith([], [agent("a", [])]), ["s"]).agent, "a");
  assert.equal(sessionExample(catalogWith([], []), ["s"]).agent, "");
});
