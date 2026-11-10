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
    assert.deepEqual(c.agents[0].enabledIn, [{ id: "p1", name: "harness", mode: "all" }], "an All project enables every agent");
    assert.deepEqual(c.agents[0].usage, { sessions: 3, zones: 1 }, "agents[].usage is carried (contract section 3)");
    assert.equal(c.agents[1].usage, null, "an agent the server sends no usage for has none");
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

  contract("the catalog read says a never-probed server's tools are unknown", async () => {
    const { gateway } = subject();
    const github = (await gateway.getCatalog()).mcpServers[0];
    assert.deepEqual([github.knownTools, github.probedAt, github.disabledTools], [null, "", []]);
  });

  contract("an MCP server save probes once, keeps the tool default and answers the probe", async () => {
    const { gateway } = subject();
    const server = { name: "github", transport: "stdio", command: "npx", args: ["-y"], disabledTools: ["search", "gone"] };
    const saved = await gateway.saveMcpServer({ previousName: "github", server, agents: ["ux-designer"], projects: ["p1"] });
    assert.deepEqual(saved.probe, { status: "ok", error: "", hint: "" });
    const github = (await gateway.getCatalog()).mcpServers[0];
    assert.deepEqual([github.knownTools, github.disabledTools], [["search"], ["search", "gone"]], "a name the tools no longer hold is kept");
    assert.ok(github.probedAt !== "", "the probe time is kept");
    const dry = await gateway.saveMcpServer({ previousName: "github", server, agents: ["ux-designer"], projects: ["p1"] }, { dryRun: true });
    assert.equal(dry.probe ?? null, null, "a dry run probes nothing");
    const absent = await gateway.saveMcpServer({ previousName: "github", server: { ...server, disabledTools: undefined }, agents: ["ux-designer"], projects: ["p1"] });
    assert.deepEqual((await gateway.getCatalog()).mcpServers[0].disabledTools, ["search", "gone"], "an absent list keeps the stored one");
    assert.equal(absent.probe?.status, "ok");
    await gateway.saveMcpServer({ previousName: "github", server: { ...server, disabledTools: [] }, agents: ["ux-designer"], projects: ["p1"] });
    assert.deepEqual((await gateway.getCatalog()).mcpServers[0].disabledTools, [], "an empty list clears it");
  });

  contract("a failed probe on save keeps the server saved and its last known tools", async () => {
    const { gateway } = subject();
    const base = { name: "github", transport: "stdio", command: "npx", disabledTools: [] };
    await gateway.saveMcpServer({ previousName: "github", server: base, agents: [], projects: [] });
    const known = (await gateway.getCatalog()).mcpServers[0];
    const saved = await gateway.saveMcpServer({ previousName: "github", server: { ...base, command: "missing-cmd" }, agents: [], projects: [] });
    assert.deepEqual([saved.probe?.status, saved.probe?.error, saved.probe?.hint], ["error", "executable not found", "Install it first."]);
    const after = (await gateway.getCatalog()).mcpServers[0];
    assert.equal(after.command, "missing-cmd");
    assert.deepEqual([after.knownTools, after.probedAt], [known.knownTools, known.probedAt]);
  });

  contract("Refresh tools probes a saved server, answers its entry and probe, and refuses an unknown name", async () => {
    const { gateway } = subject();
    const answer = await gateway.refreshMcpServerTools("github");
    assert.deepEqual(answer.probe, { status: "ok", error: "", hint: "" });
    assert.deepEqual(answer.server.knownTools, ["search"]);
    assert.equal(answer.server.name, "github");
    assert.deepEqual((await gateway.getCatalog()).mcpServers[0].knownTools, ["search"]);
    const err = await assert.rejects(gateway.refreshMcpServerTools("nope"), "UNKNOWN_REFERENCE");
    assert.deepEqual(describeRefusal(err).names, ["nope"]);
  });

  contract("a rename keeps the server's tool data under the new name", async () => {
    const { gateway } = subject();
    await gateway.refreshMcpServerTools("github");
    await gateway.saveMcpServer({ previousName: "github", server: { name: "gh", transport: "stdio", command: "npx" }, agents: [], projects: [] });
    const gh = (await gateway.getCatalog()).mcpServers.find((m) => m.name === "gh");
    assert.deepEqual(gh.knownTools, ["search"]);
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
    assert.deepEqual(dry.change.projects, [{ id: "p1", name: "harness", before: ["github"], after: ["github", "linear"], enabledAgentsBefore: [], enabledAgentsAfter: [] }]);
    assert.deepEqual(dry.change.writes.map((w) => w.path), ["mcp-servers.json"], "agents.json is untouched when no agent changes");
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github"], "a dry run leaves the project alone");
    assert.ok(!(await gateway.getCatalog()).mcpServers.some((m) => m.name === "linear"));
    const real = await gateway.saveMcpServer({ server, agents: [], projects: ["p1"] });
    assert.deepEqual(real.change.projects.map((p) => p.after), [["github", "linear"]]);
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github", "linear"]);
    const plain = await gateway.saveAgent({ agent: agent() });
    assert.deepEqual(plain.change.projects, [], "a write with no project list says so");
  });

  contract("reads a project's MCP settings: each server's state, the agents that list it and its tools with default, override and effective", async () => {
    const { gateway } = subject();
    await gateway.saveMcpServer({ previousName: "github", server: { name: "github", transport: "stdio", command: "npx", disabledTools: ["search"] }, agents: ["ux-designer"], projects: ["p1"] });
    await gateway.saveMcpServer({ server: { name: "linear", transport: "http", url: "https://linear.example/mcp" }, agents: [], projects: [] }, {});
    const settings = await gateway.getProjectMcpSettings("p1");
    assert.deepEqual(settings.added, ["github"]);
    assert.deepEqual(settings.servers.map((s) => [s.name, s.state, s.usedByAgents]), [["github", "added", ["ux-designer"]], ["linear", "agent", []]]);
    assert.deepEqual(settings.servers[0].tools, [{ name: "search", default: "disabled", override: "default", effective: "disabled" }]);
    assert.deepEqual(await gateway.getProjectMcpServers("p1"), ["github"], "the names-only read still works");
    await assert.rejects(gateway.getProjectMcpSettings("gone"), "PROJECT_NOT_FOUND");
  });

  contract("a never-probed server reads with tools null", async () => {
    const { gateway } = subject();
    assert.equal((await gateway.getProjectMcpSettings("p1")).servers[0].tools, null);
  });

  contract("setProjectMcpSettings replaces the whole settings and answers like the read", async () => {
    const { gateway } = subject();
    await gateway.refreshMcpServerTools("github");
    await gateway.saveMcpServer({ server: { name: "linear", transport: "http", url: "https://linear.example/mcp" }, agents: [], projects: [] });
    const answer = await gateway.setProjectMcpSettings("p1", [
      { name: "github", state: "banned", toolOverrides: { search: "disabled" } },
      { name: "linear", state: "added", toolOverrides: {} },
    ]);
    assert.deepEqual(answer.added, ["linear"]);
    assert.deepEqual(answer.servers.map((s) => s.state), ["banned", "added"]);
    assert.deepEqual(answer.servers[0].tools, [{ name: "search", default: "enabled", override: "disabled", effective: "disabled" }]);
    assert.deepEqual(await gateway.getProjectMcpSettings("p1"), answer);
    const again = await gateway.setProjectMcpSettings("p1", [{ name: "linear", state: "agent", toolOverrides: {} }]);
    assert.deepEqual(again.servers.map((s) => s.state), ["agent", "agent"], "a server not named is Agent's choice again");
    assert.equal(again.servers[0].tools[0].override, "default", "and its overrides are gone");
  });

  contract("setProjectMcpServers leaves bans and overrides alone", async () => {
    const { gateway } = subject();
    await gateway.refreshMcpServerTools("github");
    await gateway.saveMcpServer({ server: { name: "linear", transport: "http", url: "https://linear.example/mcp" }, agents: [], projects: [] });
    await gateway.setProjectMcpSettings("p1", [{ name: "github", state: "agent", toolOverrides: { search: "disabled" } }, { name: "linear", state: "banned", toolOverrides: {} }]);
    await gateway.setProjectMcpServers("p1", ["github"]);
    const settings = await gateway.getProjectMcpSettings("p1");
    assert.deepEqual(settings.servers.map((s) => s.state), ["added", "banned"]);
    assert.equal(settings.servers[0].tools[0].override, "disabled");
  });

  contract("setProjectMcpSettings refuses an unknown project, an unknown server, a bad value, a repeat and the services server", async () => {
    const { gateway } = subject();
    const one = (over) => [{ name: "github", state: "added", toolOverrides: {}, ...over }];
    await assert.rejects(gateway.setProjectMcpSettings("gone", one({})), "PROJECT_NOT_FOUND");
    const err = await assert.rejects(gateway.setProjectMcpSettings("p1", one({ name: "nope" })), "UNKNOWN_REFERENCE");
    assert.deepEqual(describeRefusal(err).names, ["nope"]);
    await assert.rejects(gateway.setProjectMcpSettings("p1", one({ state: "maybe" })), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setProjectMcpSettings("p1", one({ toolOverrides: { search: "default" } })), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setProjectMcpSettings("p1", [...one({}), ...one({})]), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setProjectMcpSettings("p1", one({ name: "services" })), Codes.INVALID_INPUT);
    assert.deepEqual((await gateway.getProjectMcpSettings("p1")).added, ["github"], "a refusal changes nothing");
  });

  contract("a rename carries a project's ban and overrides to the new name; a delete removes them", async () => {
    const { gateway } = subject();
    await gateway.refreshMcpServerTools("github");
    await gateway.setProjectMcpSettings("p1", [{ name: "github", state: "banned", toolOverrides: { search: "disabled" } }]);
    await gateway.saveMcpServer({ previousName: "github", server: { name: "gh", transport: "stdio", command: "npx" }, agents: [], projects: [] });
    const [gh] = (await gateway.getProjectMcpSettings("p1")).servers;
    assert.deepEqual([gh.name, gh.state, gh.tools[0].override], ["gh", "banned", "disabled"]);
    await gateway.deleteMcpServer("gh");
    assert.deepEqual((await gateway.getProjectMcpSettings("p1")).servers, []);
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

  contract("enabled_in lists each All project and each Selected project that lists the agent, and follows set_project_agents", async () => {
    const { gateway } = subject();
    const where = async (name) => (await gateway.getCatalog()).agents.find((a) => a.name === name).enabledIn;
    assert.deepEqual(await where("backend"), [{ id: "p1", name: "harness", mode: "all" }]);
    await gateway.setProjectAgents("p1", { mode: "selected", agents: ["backend"] });
    assert.deepEqual(await where("backend"), [{ id: "p1", name: "harness", mode: "selected" }]);
    assert.deepEqual(await where("ux-designer"), [], "a Selected project that does not list it does not enable it");
  });

  contract("a save with `projects` sets the Selected lists that name the agent; without it the lists stay", async () => {
    const { gateway } = subject();
    await gateway.setProjectAgents("p1", { mode: "selected", agents: ["backend"] });
    await gateway.saveAgent({ agent: agent() });
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "selected", agents: ["backend"] }, "no `projects`: left as it was, so a new agent is not enabled there");
    const dry = await gateway.saveAgent({ agent: agent({ name: "reviewer" }), previousName: "reviewer", projects: ["p1"] }, { dryRun: true });
    assert.deepEqual(dry.change.projects, [{ id: "p1", name: "harness", before: ["github"], after: ["github"], enabledAgentsBefore: ["backend"], enabledAgentsAfter: ["backend", "reviewer"] }]);
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "selected", agents: ["backend"] }, "a dry run changes nothing");
    const real = await gateway.saveAgent({ agent: agent(), previousName: "reviewer", projects: ["p1"] });
    assert.deepEqual(real.change.projects.map((p) => p.enabledAgentsAfter), [["backend", "reviewer"]]);
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "selected", agents: ["backend", "reviewer"] });
    await gateway.saveAgent({ agent: agent(), previousName: "reviewer", projects: [] });
    assert.deepEqual(await gateway.getProjectAgents("p1"), { mode: "selected", agents: ["backend"] }, "an empty set takes the agent off every list");
  });

  contract("naming an All project in `projects` is INVALID_INPUT at the field; an unknown project is UNKNOWN_REFERENCE", async () => {
    const { gateway } = subject();
    const err = await assert.rejects(gateway.saveAgent({ agent: agent(), projects: ["p1"] }), Codes.INVALID_INPUT);
    assert.equal(err.details.field, "projects");
    assert.equal(describeRefusal(err).field, "projects");
    assert.ok(!(await gateway.getCatalog()).agents.some((a) => a.name === "reviewer"), "a refused save writes nothing");
    const gone = await assert.rejects(gateway.saveAgent({ agent: agent(), projects: ["nope"] }), "UNKNOWN_REFERENCE");
    assert.deepEqual(gone.details.names, ["nope"]);
  });

  contract("renaming and deleting an agent show in change.projects with the enabled agents before and after", async () => {
    const { gateway } = subject();
    await gateway.setProjectAgents("p1", { mode: "selected", agents: ["backend"] });
    const renamed = await gateway.saveAgent({ previousName: "backend", agent: agent({ name: "api", entries: ["tdd"] }) });
    assert.deepEqual(renamed.change.projects.map((p) => [p.id, p.enabledAgentsBefore, p.enabledAgentsAfter]), [["p1", ["backend"], ["api"]]]);
    const deleted = await gateway.deleteAgent("api");
    assert.deepEqual(deleted.change.projects.map((p) => [p.enabledAgentsBefore, p.enabledAgentsAfter]), [[["api"], []]]);
  });
}
