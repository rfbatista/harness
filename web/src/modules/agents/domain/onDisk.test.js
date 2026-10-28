import { assert, file, test } from "../../../shared/testing/test.js";
import { makeWorld } from "../testing/fixtures.js";
import { memoryCatalog } from "../infrastructure/memory-gateway.js";
import { catalogChanges, changeSentence, movedFiles } from "./onDisk.js";

file("agents/domain/onDisk");

const clone = (c) => structuredClone(c);

test("nothing differs: no change", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const c = await gateway.getCatalog();
  assert.deepEqual(catalogChanges(c, clone(c)), {});
});

test("an agent's own fields are named, with what was added and removed", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const before = await gateway.getCatalog();
  const after = clone(before);
  const a = after.agents.find((x) => x.name === "ux-designer");
  a.description = "Designs screens well.";
  a.entries = ["@ux", "lint"];
  a.allowedTools = ["Read", "Bash"];
  const changes = catalogChanges(before, after);
  assert.deepEqual(Object.keys(changes), ["agent:ux-designer"]);
  assert.deepEqual(changes["agent:ux-designer"].lines, [
    "description changed",
    "skills and bundles: added lint, removed tdd",
    "allowed tools: added Bash",
  ]);
  assert.equal(changeSentence(changes["agent:ux-designer"]), "This agent changed on disk.");
});

test("a skill is not changed because an agent started naming it, but its description is", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const before = await gateway.getCatalog();
  const after = clone(before);
  after.skills.find((s) => s.name === "tdd").agents = [];
  after.skills.find((s) => s.name === "tdd").agentCount = 0;
  assert.deepEqual(catalogChanges(before, after), {});
  after.skills.find((s) => s.name === "lint").description = "Lint harder.";
  assert.deepEqual(Object.keys(catalogChanges(before, after)), ["skill:lint"]);
});

test("a deleted item is a change that says so; a new one is not", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const before = await gateway.getCatalog();
  const after = clone(before);
  after.bundles = [];
  after.agents.push({ ...after.agents[0], name: "fresh" });
  const changes = catalogChanges(before, after);
  assert.deepEqual(Object.keys(changes), ["bundle:ux"]);
  assert.equal(changes["bundle:ux"].gone, true);
  assert.equal(changeSentence(changes["bundle:ux"]), "This bundle was removed from the catalog on disk.");
});

test("no line carries the value of a variable, header, command or URL", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const before = await gateway.getCatalog();
  const after = clone(before);
  const s = after.mcpServers[0];
  s.env = { TOKEN: "ghp_supersecret" };
  s.command = "npx --token ghp_supersecret";
  s.url = "https://x.example/?key=ghp_supersecret";
  s.headers = { Authorization: "Bearer ghp_supersecret" };
  const lines = catalogChanges(before, after)["mcp:github"].lines;
  assert.deepEqual(lines, ["command changed", "environment variables changed", "URL changed", "headers changed"]);
  assert.ok(!JSON.stringify(lines).includes("ghp_"));
});

test("a file this page wrote itself is not a change from outside, and neither is a skill it saved", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const before = await gateway.getCatalog();
  const after = clone(before);
  after.versions["agents.json"] = "sha256:mine";
  after.agents[0].description = "Mine.";
  after.bundles[0].entries = ["tdd"];
  after.skills[1].description = "Mine too.";
  assert.deepEqual(catalogChanges(before, after, { hashes: new Set(["sha256:mine"]), skills: new Set(["lint"]) }), {});
  assert.equal(Object.keys(catalogChanges(before, after, { hashes: new Set(["sha256:other"]) })).length, 3);
});

test("movedFiles names the files an editor read that have another version now", () => {
  assert.deepEqual(movedFiles({ "agents.json": "a", "mcp-servers.json": "m" }, { "agents.json": "b", "mcp-servers.json": "m" }), ["agents.json"]);
  assert.deepEqual(movedFiles({ "agents.json": "a" }, {}), []);
  assert.deepEqual(movedFiles({}, { "agents.json": "b" }), []);
});
