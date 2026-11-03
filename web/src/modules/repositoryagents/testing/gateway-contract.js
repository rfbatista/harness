// The RepositoryAgentsGateway contract (../domain/ports.js), run by every
// implementation. Contract: "Harness server ↔ Web UI — repository agents and
// repository prompts".

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { makeWorld } from "./fixtures.js";

/**
 * @param {string} name
 * @param {(world: object) => { gateway: import("../domain/ports.js").RepositoryAgentsGateway, drop?: () => void, resync?: () => void }} makeSubject
 */
export function repositoryAgentsGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const subject = (over) => makeSubject(makeWorld(over));
  const byName = (tab) => Object.fromEntries(tab.agents.map((a) => [a.name, a]));

  contract("reads the Agents tab: groups by state, prompts, running sessions, the list version", async () => {
    const { gateway } = subject();
    const tab = await gateway.getRepositoryAgents("r1");
    assert.deepEqual(tab.repository, { id: "r1", name: "atlas-api" });
    assert.deepEqual(tab.project, { id: "p1", name: "atlas", agentsMode: "selected", enabledCount: 3, catalogCount: 5 });
    assert.deepEqual(tab.agents.map((a) => [a.name, a.state]), [
      ["reviewer", "assigned"],
      ["tester", "assigned"],
      ["backend", "unassigned"],
      ["docs", "not_enabled"],
    ]);
    const a = byName(tab);
    assert.deepEqual(a.reviewer.prompt, { set: true, summary: "Run make check before every commit.", length: 53, version: a.reviewer.prompt.version });
    assert.ok(a.reviewer.prompt.version !== "", "a saved prompt has a version");
    assert.deepEqual(a.tester.prompt, { set: false, summary: "", length: 0, version: "" });
    assert.equal(a.reviewer.runningSessions, 1);
    assert.equal(a.reviewer.previousName, "rev");
    assert.equal(a.tester.previousName, "");
    assert.equal(a.backend.unassigned, true);
    assert.equal(a.backend.prompt.set, true, "an unassigned agent keeps its prompt");
    assert.equal(a.docs.unassigned, true, "the list itself is independent of the state");
    assert.equal(a.ops, undefined, "neither enabled nor unassigned: not listed");
    assert.ok(tab.listVersion !== "");
    await assert.rejects(gateway.getRepositoryAgents("ghost"), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("unassign moves the agent, answers the tab with its running sessions, and changes the list version", async () => {
    const { gateway } = subject();
    const before = await gateway.getRepositoryAgents("r1");
    const after = await gateway.unassignAgent({ repositoryId: "r1", agent: "reviewer", listVersion: before.listVersion });
    assert.equal(byName(after).reviewer.state, "unassigned");
    assert.equal(after.runningSessions, 1, "the sessions that continue");
    assert.ok(after.listVersion !== before.listVersion);
    assert.equal(byName(after).reviewer.prompt.set, true, "its prompt is kept");
    assert.deepEqual(byName(await gateway.getRepositoryAgents("r1")).reviewer, byName(after).reviewer, "a read gives the same row");
  });

  contract("assign again, and assign all again, put agents back", async () => {
    const { gateway } = subject();
    let tab = await gateway.getRepositoryAgents("r1");
    tab = await gateway.assignAgentAgain({ repositoryId: "r1", agent: "backend", listVersion: tab.listVersion });
    assert.equal(byName(tab).backend.state, "assigned");
    tab = await gateway.unassignAgent({ repositoryId: "r1", agent: "reviewer", listVersion: tab.listVersion });
    tab = await gateway.unassignAgent({ repositoryId: "r1", agent: "tester", listVersion: tab.listVersion });
    tab = await gateway.assignAllAgentsAgain({ repositoryId: "r1", listVersion: tab.listVersion });
    assert.deepEqual(tab.agents.filter((a) => a.state === "unassigned"), []);
    assert.equal(byName(tab).docs, undefined, "assigning again does not enable; it leaves the list");
  });

  contract("repeating an unassign or an assign again is accepted and keeps the list version", async () => {
    const { gateway } = subject();
    const tab = await gateway.getRepositoryAgents("r1");
    const again = await gateway.assignAgentAgain({ repositoryId: "r1", agent: "reviewer", listVersion: tab.listVersion });
    assert.equal(again.listVersion, tab.listVersion);
    const once = await gateway.unassignAgent({ repositoryId: "r1", agent: "backend", listVersion: tab.listVersion });
    assert.equal(once.listVersion, tab.listVersion, "backend was already unassigned");
  });

  contract("an agent the project does not enable can be unassigned", async () => {
    const { gateway } = subject();
    const tab = await gateway.getRepositoryAgents("r1");
    const after = await gateway.unassignAgent({ repositoryId: "r1", agent: "ops", listVersion: tab.listVersion });
    assert.equal(byName(after).ops.state, "not_enabled", "the state is not_enabled whatever the list says");
    assert.equal(byName(after).ops.unassigned, true);
  });

  contract("a stale list version is refused with the current state, and nothing is applied", async () => {
    const { gateway } = subject();
    const old = await gateway.getRepositoryAgents("r1");
    await gateway.unassignAgent({ repositoryId: "r1", agent: "tester", listVersion: old.listVersion });
    const err = await assert.rejects(gateway.unassignAgent({ repositoryId: "r1", agent: "reviewer", listVersion: old.listVersion }), Codes.REPOSITORY_AGENTS_CHANGED);
    assert.equal(byName(err.details.current).tester.state, "unassigned", "the refusal carries the current tab");
    assert.equal(byName(err.details.current).reviewer.state, "assigned", "nothing was applied");
    await assert.rejects(gateway.assignAgentAgain({ repositoryId: "r1", agent: "tester", listVersion: old.listVersion }), Codes.REPOSITORY_AGENTS_CHANGED);
    await assert.rejects(gateway.assignAllAgentsAgain({ repositoryId: "r1", listVersion: old.listVersion }), Codes.REPOSITORY_AGENTS_CHANGED);
  });

  contract("an unknown agent or repository is refused", async () => {
    const { gateway } = subject();
    const tab = await gateway.getRepositoryAgents("r1");
    await assert.rejects(gateway.unassignAgent({ repositoryId: "r1", agent: "ghost", listVersion: tab.listVersion }), "AGENT_NOT_FOUND");
    await assert.rejects(gateway.unassignAgent({ repositoryId: "ghost", agent: "reviewer", listVersion: "" }), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("reads a repository prompt with its version, the limit and whether the agent is unassigned", async () => {
    const { gateway } = subject();
    const p = await gateway.getRepositoryPrompt("r1", "backend");
    assert.deepEqual([p.agent, p.repositoryId, p.content, p.limit, p.unassigned], ["backend", "r1", "Mind the migrations.", 8000, true]);
    assert.ok(p.version !== "");
    const empty = await gateway.getRepositoryPrompt("r1", "tester");
    assert.deepEqual([empty.content, empty.version, empty.unassigned], ["", "", false]);
    await assert.rejects(gateway.getRepositoryPrompt("r1", "ghost"), "AGENT_NOT_FOUND");
  });

  contract("saves a prompt and moves its version; the tab shows its summary", async () => {
    const { gateway } = subject();
    const p = await gateway.getRepositoryPrompt("r1", "tester");
    const saved = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "Cover the edge cases.\nThen the rest.", version: p.version });
    assert.equal(saved.content, "Cover the edge cases.\nThen the rest.");
    assert.ok(saved.version !== "");
    const tab = byName(await gateway.getRepositoryAgents("r1")).tester;
    assert.deepEqual([tab.prompt.set, tab.prompt.summary, tab.prompt.version], [true, "Cover the edge cases.", saved.version]);
    const next = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "Changed.", version: saved.version });
    assert.ok(next.version !== saved.version);
  });

  contract("a stale prompt version is refused with the current prompt; the draft is not applied", async () => {
    const { gateway } = subject();
    const old = await gateway.getRepositoryPrompt("r1", "reviewer");
    await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: "Theirs.", version: old.version });
    const err = await assert.rejects(gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: "Mine.", version: old.version }), Codes.REPOSITORY_PROMPT_CHANGED);
    assert.equal(err.details.current.content, "Theirs.");
    assert.equal((await gateway.getRepositoryPrompt("r1", "reviewer")).content, "Theirs.");
    const fresh = await gateway.getRepositoryPrompt("r1", "tester");
    await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "x", version: fresh.version });
    await assert.rejects(gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "y", version: "" }), Codes.REPOSITORY_PROMPT_CHANGED);
  });

  contract("a blank save clears the prompt, and its version is empty", async () => {
    const { gateway } = subject();
    const p = await gateway.getRepositoryPrompt("r1", "reviewer");
    const cleared = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: " \n\t ", version: p.version });
    assert.deepEqual([cleared.content, cleared.version], ["", ""]);
    assert.equal(byName(await gateway.getRepositoryAgents("r1")).reviewer.prompt.set, false);
  });

  contract("a prompt over the limit is refused with the limit and the length", async () => {
    const { gateway } = subject();
    const err = await assert.rejects(gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "x".repeat(8001), version: "" }), Codes.REPOSITORY_PROMPT_TOO_LONG);
    assert.deepEqual([err.details.limit, err.details.length], [8000, 8001]);
    const ok = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "x".repeat(8000), version: "" });
    assert.equal(ok.content.length, 8000);
  });

  contract("a prompt can be saved for an unassigned or not enabled agent", async () => {
    const { gateway } = subject();
    const p = await gateway.getRepositoryPrompt("r1", "docs");
    const saved = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "docs", content: "Kept.", version: p.version });
    assert.equal(saved.content, "Kept.");
  });

  contract("previews unsaved content with the server's agent prompt and repository section, and stores nothing", async () => {
    const { gateway } = subject();
    const pv = await gateway.previewRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "Draft only." });
    assert.equal(pv.heading, "## Repository prompt: atlas-api");
    assert.ok(pv.agentPrompt.length > 0);
    assert.ok(pv.repositorySection.startsWith(pv.heading), "the section carries its heading");
    assert.ok(pv.repositorySection.includes("Draft only."));
    const blank = await gateway.previewRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "  " });
    assert.equal(blank.repositorySection, "");
    assert.equal((await gateway.getRepositoryPrompt("r1", "tester")).content, "", "nothing stored");
  });

  contract("every write publishes repository_agents_changed; a read does not", async () => {
    const { gateway } = subject();
    const seen = [];
    const stop = gateway.followChanges((c) => seen.push(c));
    await flush();
    const tab = await gateway.getRepositoryAgents("r1");
    await flush();
    assert.deepEqual(seen, []);
    await gateway.unassignAgent({ repositoryId: "r1", agent: "reviewer", listVersion: tab.listVersion });
    await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "tester", content: "x", version: "" });
    await flush();
    assert.deepEqual(seen.map((c) => [c.kind, c.projectId, c.repositoryId, c.agents]), [
      ["changed", "p1", "r1", ["reviewer"]],
      ["changed", "p1", "r1", ["tester"]],
    ]);
    stop();
    await gateway.assignAgentAgain({ repositoryId: "r1", agent: "reviewer", listVersion: (await gateway.getRepositoryAgents("r1")).listVersion });
    await flush();
    assert.equal(seen.length, 2, "a stopped follower hears nothing");
  });

  contract("a repeated unassign publishes nothing; a resync is surfaced so views re-read", async () => {
    const { gateway, resync } = subject();
    const seen = [];
    const statuses = [];
    gateway.followChanges((c) => seen.push(c), (s) => statuses.push(s));
    await flush();
    const tab = await gateway.getRepositoryAgents("r1");
    await gateway.unassignAgent({ repositoryId: "r1", agent: "backend", listVersion: tab.listVersion });
    await flush();
    assert.deepEqual(seen, []);
    resync?.();
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE));
    if (resync) assert.equal(statuses.at(-1), FeedStatus.RESYNCED);
  });

  contract("decoders refuse a malformed answer with BAD_RESPONSE", async () => {
    const { gateway } = subject();
    assert.throws(() => gateway.decodeRepositoryAgents(null), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeRepositoryAgents({ repository: { id: "r" }, agents: "x" }), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeRepositoryPrompt({ agent: 3 }), Codes.BAD_RESPONSE);
    const tab = gateway.decodeRepositoryAgents({ repository: { id: "r1", name: "a" }, project: { id: "p", name: "p", agents_mode: "all" }, list_version: "1", agents: null });
    assert.deepEqual(tab.agents, []);
  });
}
