// The CatalogGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the server as far as the
// Agents page can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";
import { describeRefusal } from "../domain/refusals.js";
import { catalogDTO, makeWorld } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").CatalogGateway} gateway
 * @property {ReturnType<typeof import("../infrastructure/memory-gateway.js").memoryCatalogServer>} server  the server's side
 */

const agent = (over = {}) => ({ name: "reviewer", description: "Reviews code.", aliases: [], entries: ["tdd"], mcpServers: [], allowedTools: [], ...over });

/**
 * @param {string} name
 * @param {(world: object) => Subject} makeSubject
 */
export function catalogGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const subject = () => makeSubject(makeWorld());

  contract("reads the catalog in camelCase, with the root, versions and problems", async () => {
    const { gateway } = subject();
    const c = await gateway.getCatalog();
    assert.equal(c.root, "/data/agents");
    assert.equal(c.versions["agents.json"], "sha256:a1");
    assert.deepEqual(c.agents.map((a) => a.name), ["ux-designer", "backend"]);
    assert.deepEqual(c.agents[1].problems.map((p) => p.code), ["NO_DESCRIPTION", "MISSING_SKILL"]);
    assert.deepEqual(c.agents[0].resolvedSkills, ["lint", "tdd"]);
    assert.equal(c.skills[0].hasExtraFiles, true);
    assert.deepEqual(c.mcpServers[0].envRefs, [{ name: "GITHUB_TOKEN", set: false }]);
    assert.deepEqual(c.mcpServers[0].projects, [{ id: "p1", name: "harness" }]);
    assert.deepEqual(c.builtInTools.map((t) => t.name), ["Read", "Bash"]);
    assert.deepEqual(gateway.decodeCatalog(JSON.parse(JSON.stringify(catalogDTO()))).agents.map((a) => a.name), ["ux-designer", "backend"]);
  });

  contract("an empty catalog is empty lists, and a malformed one is BAD_RESPONSE", async () => {
    const { gateway } = subject();
    const empty = gateway.decodeCatalog({ root: "/d", versions: {}, broken: null, agents: null, bundles: null, skills: null, mcp_servers: null, built_in_tools: null });
    assert.deepEqual([empty.agents, empty.bundles, empty.skills, empty.mcpServers], [[], [], [], []]);
    assert.throws(() => gateway.decodeCatalog(null), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeCatalog({ agents: "x" }), Codes.BAD_RESPONSE);
  });

  contract("reads a skill; an unknown one is SKILL_NOT_FOUND", async () => {
    const { gateway } = subject();
    const { skill } = await gateway.getSkill("tdd");
    assert.deepEqual([skill.name, skill.agentCount, skill.agents], ["tdd", 2, ["ux-designer", "backend"]]);
    await assert.rejects(gateway.getSkill("nope"), "SKILL_NOT_FOUND");
  });

  contract("creates an agent, answers the change, and refuses a taken name with a suggestion", async () => {
    const { gateway } = subject();
    const r = await gateway.saveAgent({ agent: agent() });
    assert.ok(r.change.id !== "", "a saved change has an id");
    assert.ok(r.change.summary.startsWith("Saved agents.json"), r.change.summary);
    assert.equal(r.change.writes[0].path, "agents.json");
    assert.ok(r.change.writes[0].before !== r.change.writes[0].after);
    assert.ok((await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"));
    const err = await assert.rejects(gateway.saveAgent({ agent: agent() }), "NAME_TAKEN");
    assert.equal(describeRefusal(err).suggestion, "reviewer-2");
  });

  contract("refuses a bad name or description at its field, and an unknown reference with its names", async () => {
    const { gateway } = subject();
    let err = await assert.rejects(gateway.saveAgent({ agent: agent({ name: "Bad Name" }) }), Codes.INVALID_INPUT);
    assert.equal(describeRefusal(err).field, "name");
    err = await assert.rejects(gateway.saveAgent({ agent: agent({ description: " " }) }), Codes.INVALID_INPUT);
    assert.equal(describeRefusal(err).field, "description");
    err = await assert.rejects(gateway.saveAgent({ agent: agent({ entries: ["tdd", "@gone"], mcpServers: ["nope"] }) }), "UNKNOWN_REFERENCE");
    assert.deepEqual(describeRefusal(err).names, ["@gone"]);
    await assert.rejects(gateway.saveAgent({ previousName: "ghost", agent: agent() }), "AGENT_NOT_FOUND");
  });

  contract("renaming a bundle rewrites every @name; a bundle that includes itself is BUNDLE_CYCLE", async () => {
    const { gateway } = subject();
    await gateway.saveBundle({ previousName: "ux", bundle: { name: "design", entries: ["lint"] } });
    const c = await gateway.getCatalog();
    assert.deepEqual(c.bundles.map((b) => b.name), ["design"]);
    assert.deepEqual(c.agents[0].entries, ["@design", "tdd"]);
    await gateway.saveBundle({ bundle: { name: "inner", entries: ["@design"] } });
    await assert.rejects(gateway.saveBundle({ previousName: "design", bundle: { name: "design", entries: ["@inner"] } }), "BUNDLE_CYCLE");
    await assert.rejects(gateway.saveBundle({ previousName: "design", bundle: { name: "design", entries: ["@design"] } }), "BUNDLE_CYCLE");
  });

  contract("a skill save sets who gets it; a delete removes every reference", async () => {
    const { gateway } = subject();
    await gateway.saveSkill({ skill: { name: "review", description: "Review.", body: "x", files: [], deletedFiles: [] }, addTo: { agents: ["backend"], bundles: ["ux"] } });
    let c = await gateway.getCatalog();
    assert.deepEqual(c.skills.find((s) => s.name === "review").agents, ["backend"]);
    assert.deepEqual(c.skills.find((s) => s.name === "review").bundles, ["ux"]);
    await gateway.deleteSkill("review");
    c = await gateway.getCatalog();
    assert.ok(!c.skills.some((s) => s.name === "review"));
    assert.ok(!c.bundles[0].entries.includes("review"));
    await assert.rejects(gateway.deleteSkill("review"), "SKILL_NOT_FOUND");
  });

  contract("deleting a bundle or an agent answers a change with an id; Undo puts it back, and a stale base is CATALOG_CHANGED", async () => {
    const { gateway, server } = subject();
    const { versions } = await gateway.getCatalog();
    let r = await gateway.deleteBundle("ux", { baseVersions: { "agents.json": versions["agents.json"] } });
    assert.ok(r.change.id !== "", "a delete has a change id");
    assert.ok(r.change.summary.startsWith("Deleted"), r.change.summary);
    let c = await gateway.getCatalog();
    assert.deepEqual(c.bundles, []);
    assert.deepEqual(c.agents[0].entries, ["tdd"], "the @ux entry left the agent");
    await gateway.undoChange(r.change.id);
    c = await gateway.getCatalog();
    assert.deepEqual(c.bundles.map((b) => b.name), ["ux"]);
    assert.deepEqual(c.agents[0].entries, ["@ux", "tdd"]);

    r = await gateway.deleteAgent("backend");
    assert.deepEqual((await gateway.getCatalog()).agents.map((a) => a.name), ["ux-designer"]);
    await gateway.undoChange(r.change.id);
    assert.deepEqual((await gateway.getCatalog()).agents.map((a) => a.name), ["ux-designer", "backend"]);
    await assert.rejects(gateway.deleteAgent("ghost"), "AGENT_NOT_FOUND");
    await assert.rejects(gateway.deleteBundle("ghost"), "BUNDLE_NOT_FOUND");

    const stale = (await gateway.getCatalog()).versions;
    server.editOnDisk("agents.json");
    const err = await assert.rejects(gateway.deleteBundle("ux", { baseVersions: stale }), "CATALOG_CHANGED");
    assert.equal(describeRefusal(err).changed[0].path, "agents.json");
    assert.deepEqual((await gateway.getCatalog()).bundles.map((b) => b.name), ["ux"], "nothing was deleted");
    await gateway.deleteBundle("ux", { baseVersions: stale, force: true });
    assert.deepEqual((await gateway.getCatalog()).bundles, []);
  });

  contract("an MCP server save sets which agents and projects get it; a delete takes it off them", async () => {
    const { gateway } = subject();
    const server = { name: "linear", transport: "http", url: "https://linear.example/mcp", headers: { Authorization: "Bearer ${LINEAR_TOKEN}" } };
    await gateway.saveMcpServer({ server, agents: ["backend"], projects: ["p1"] });
    let c = await gateway.getCatalog();
    const linear = c.mcpServers.find((m) => m.name === "linear");
    assert.deepEqual([linear.agents, linear.projects.map((p) => p.id), linear.envRefs], [["backend"], ["p1"], [{ name: "LINEAR_TOKEN", set: false }]]);
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github", "linear"]);
    await gateway.deleteMcpServer("github");
    c = await gateway.getCatalog();
    assert.deepEqual(c.agents[0].mcpServers, []);
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["linear"]);
    await assert.rejects(gateway.saveMcpServer({ server: { name: "x", transport: "stdio", command: "" }, agents: [], projects: [] }), Codes.INVALID_INPUT);
  });

  contract("a dry run writes nothing and has no change id", async () => {
    const { gateway } = subject();
    const r = await gateway.saveAgent({ agent: agent() }, { dryRun: true });
    assert.equal(r.change.id, "");
    assert.ok(r.change.writes[0].after.includes("reviewer"));
    assert.ok(!(await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"));
  });

  contract("a write whose base versions are stale is CATALOG_CHANGED with what is on disk; force overwrites", async () => {
    const { gateway, server } = subject();
    const { versions } = await gateway.getCatalog();
    server.editOnDisk("agents.json");
    const err = await assert.rejects(gateway.saveAgent({ agent: agent() }, { baseVersions: versions }), "CATALOG_CHANGED");
    const refusal = describeRefusal(err);
    assert.ok(refusal.currentVersions["agents.json"] !== versions["agents.json"]);
    assert.equal(refusal.changed[0].path, "agents.json");
    assert.ok(!(await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"));
    await gateway.saveAgent({ agent: agent() }, { baseVersions: versions, force: true });
    assert.ok((await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"));
  });

  contract("a write that touches a broken file is CATALOG_BROKEN, and the read still answers", async () => {
    const { gateway, server } = subject();
    server.breakFile("agents.json", 12, 4, "unexpected }");
    const c = await gateway.getCatalog();
    assert.deepEqual(c.broken, { path: "agents.json", line: 12, column: 4, message: "unexpected }" });
    await assert.rejects(gateway.saveAgent({ agent: agent() }), "CATALOG_BROKEN");
    await assert.rejects(gateway.saveAgent({ agent: agent() }, { force: true }), "CATALOG_BROKEN");
  });

  contract("Undo restores the latest change only, and refuses an older, unknown or outdated one", async () => {
    const { gateway, server } = subject();
    const first = await gateway.saveAgent({ agent: agent() });
    const second = await gateway.saveAgent({ agent: agent({ name: "other" }) });
    await assert.rejects(gateway.undoChange(first.change.id), "CHANGE_NOT_UNDOABLE");
    await assert.rejects(gateway.undoChange("nope"), "CHANGE_NOT_UNDOABLE");
    server.editOnDisk("agents.json");
    await assert.rejects(gateway.undoChange(second.change.id), "CATALOG_CHANGED");
  });

  contract("Undo puts the file back", async () => {
    const { gateway } = subject();
    const saved = await gateway.saveAgent({ agent: agent() });
    await gateway.undoChange(saved.change.id);
    assert.ok(!(await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"));
    await assert.rejects(gateway.undoChange(saved.change.id), "CHANGE_NOT_UNDOABLE");
  });

  contract("checks environment variables without ever answering a value", async () => {
    const { gateway } = subject();
    assert.deepEqual(await gateway.checkEnvVars(["GITHUB_TOKEN", "HOME"]), { GITHUB_TOKEN: false, HOME: true });
  });

  contract("tests an unsaved MCP server", async () => {
    const { gateway } = subject();
    const ok = await gateway.testMcpServer({ name: "x", transport: "stdio", command: "npx", args: [] });
    assert.deepEqual([ok.status, ok.tools.map((t) => t.name)], ["ok", ["search"]]);
    const bad = await gateway.testMcpServer({ name: "x", transport: "stdio", command: "missing-cmd" });
    assert.deepEqual([bad.status, bad.error, bad.hint], ["error", "executable not found", "Install it first."]);
  });

  contract("lists the projects a server can be given to", async () => {
    const { gateway } = subject();
    assert.deepEqual(await gateway.listProjects(), [{ id: "p1", name: "harness" }]);
  });

  contract("an MCP server's dry run names the project lists it would set and changes nothing", async () => {
    const { gateway } = subject();
    const server = { name: "linear", transport: "http", url: "https://linear.example/mcp", headers: {} };
    const dry = await gateway.saveMcpServer({ server, agents: [], projects: ["p1"] }, { dryRun: true });
    assert.equal(dry.change.id, "");
    assert.deepEqual(dry.change.projects, [{ id: "p1", name: "harness", before: ["github"], after: ["github", "linear"] }]);
    assert.deepEqual(dry.change.writes.map((w) => w.path), ["mcp-servers.json"], "agents.json is untouched when no agent changes");
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github"], "a dry run leaves the project alone");
    assert.ok(!(await gateway.getCatalog()).mcpServers.some((m) => m.name === "linear"));
    const real = await gateway.saveMcpServer({ server, agents: [], projects: ["p1"] });
    assert.deepEqual(real.change.projects.map((p) => p.after), [["github", "linear"]]);
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github", "linear"]);
    const plain = await gateway.saveAgent({ agent: agent() });
    assert.deepEqual(plain.change.projects, [], "a write with no project list says so");
  });

  contract("sets a project's MCP servers; an unknown project or server is refused", async () => {
    const { gateway } = subject();
    assert.deepEqual(await gateway.setProjectMcpServers("p1", []), []);
    assert.deepEqual(await gateway.setProjectMcpServers("p1", ["github"]), ["github"]);
    await assert.rejects(gateway.getProjectMcpServers("gone"), "PROJECT_NOT_FOUND");
    await assert.rejects(gateway.setProjectMcpServers("p1", ["nope"]), "UNKNOWN_REFERENCE");
  });

  contract("a project's enabled agents default to all; Selected keeps its list, All stores none", async () => {
    const { gateway } = subject();
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "all", agents: [] });
    assert.deepEqual(await gateway.setProjectAgents("p1", { mode: "selected", agents: ["backend"] }), { mode: "selected", agents: ["backend"] });
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "selected", agents: ["backend"] });
    assert.deepEqual(await gateway.setProjectAgents("p1", { mode: "selected", agents: [] }), { mode: "selected", agents: [] }, "an empty Selected list is allowed");
    assert.deepEqual(await gateway.setProjectAgents("p1", { mode: "all", agents: ["backend"] }), { mode: "all", agents: [] });
  });

  contract("an unknown project or agent is refused when setting a project's agents", async () => {
    const { gateway } = subject();
    await assert.rejects(gateway.getProjectAgents("gone"), "PROJECT_NOT_FOUND");
    await assert.rejects(gateway.setProjectAgents("gone", { mode: "all", agents: [] }), "PROJECT_NOT_FOUND");
    const err = await assert.rejects(gateway.setProjectAgents("p1", { mode: "selected", agents: ["backend", "ghost"] }), "UNKNOWN_REFERENCE");
    assert.deepEqual(err.details.names, ["ghost"]);
  });
}
