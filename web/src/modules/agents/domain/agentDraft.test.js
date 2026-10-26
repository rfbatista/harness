import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import {
  CREATE_SKILL, agentChanges, agentDraft, agentFieldErrors, agentPickerOptions, checkAgentDraft, createSkillHref,
  expandEntries, mcpChoices, resolvedSummary, toolChoices, tocCounts,
} from "./agentDraft.js";
import { describeRefusal } from "./refusals.js";

file("agents/domain/agentDraft");

const bundle = (name, entries) => ({ name, entries, resolvedSkills: [], usedByAgents: [], usedByBundles: [] });

// Worked example: ux holds @laws and lint; laws holds hicks-law and fitts-law;
// landing holds lint again and copy.
const catalog = {
  root: "/data/agents",
  versions: { "agents.json": "sha256:a1" },
  agents: [
    { name: "ux-designer", description: "Designs.", aliases: ["ux"], entries: ["@ux", "tdd"], mcpServers: ["github"], allowedTools: ["Read"] },
    { name: "backend", description: "", aliases: [], entries: ["tdd", "ghost"], mcpServers: [], allowedTools: [] },
  ],
  bundles: [bundle("ux", ["@laws", "lint"]), bundle("laws", ["hicks-law", "fitts-law"]), bundle("landing", ["lint", "copy"])],
  skills: ["lint", "hicks-law", "fitts-law", "copy", "tdd", "dev-browser"].map((name) => ({ name, description: `${name} skill.` })),
  mcpServers: [
    { name: "github", transport: "stdio", envRefs: [{ name: "GITHUB_TOKEN", set: false }] },
    { name: "docs", transport: "http", envRefs: [] },
  ],
  builtInTools: [{ name: "Read", description: "Reads a file." }, { name: "Bash", description: "Runs a command." }],
};

test("a bundle expands in place, nested bundles too, and the first of a duplicate stays", () => {
  const r = expandEntries(catalog, ["@ux", "tdd", "lint", "@landing"]);
  assert.deepEqual(r.resolved, ["hicks-law", "fitts-law", "lint", "tdd", "copy"]);
  assert.deepEqual(r.dropped, ["lint"], "lint comes in with @ux, again alone and again in @landing: dropped once in the list");
  assert.deepEqual(r.missing, []);
});

test("an entry the catalog lacks is missing, not counted; a bundle that holds itself does not loop", () => {
  const r = expandEntries({ ...catalog, bundles: [bundle("a", ["@b", "tdd"]), bundle("b", ["@a", "lint"])] }, ["@a", "ghost", "@gone"]);
  assert.deepEqual(r.resolved, ["lint", "tdd"]);
  assert.deepEqual(r.missing, ["ghost", "@gone"]);
});

test("the summary names the total after bundles, the duplicates dropped and what is missing", () => {
  const s = resolvedSummary(catalog, ["@ux", "lint", "tdd"]);
  assert.equal(s.total, 4);
  assert.equal(s.text, "4 skills after bundles are expanded · 1 duplicate dropped (lint)");
  assert.equal(resolvedSummary(catalog, ["tdd"]).text, "1 skill after bundles are expanded");
  assert.equal(resolvedSummary(catalog, []).text, "No skills yet");
  assert.equal(resolvedSummary(catalog, ["tdd", "ghost"]).text, "1 skill after bundles are expanded · 1 missing (ghost)");
});

test("the picker lists bundles first, then skills, and marks a skill a chosen bundle already brings", () => {
  const o = agentPickerOptions(catalog, { query: "", entries: ["@ux"] });
  assert.deepEqual(o.options.map((x) => x.key), ["@landing", "@laws", "@ux", "copy", "dev-browser", "fitts-law", "hicks-law", "lint", "tdd", CREATE_SKILL]);
  const by = Object.fromEntries(o.options.map((x) => [x.key, x]));
  assert.equal(by["@ux"].chosen, true);
  assert.equal(by["@ux"].hint, "3 skills");
  assert.equal(by["lint"].hint, "already in @ux");
  assert.equal(by["hicks-law"].hint, "already in @ux", "through the nested bundle");
  assert.equal(by["tdd"].hint, "");
  assert.equal(by["lint"].chosen, false, "it is not chosen on its own");
  assert.equal(by[CREATE_SKILL].label, "Create a new skill…");
});

test("typing narrows by name or description; no match offers to create the skill typed", () => {
  const some = agentPickerOptions(catalog, { query: "law", entries: [] });
  assert.deepEqual(some.options.map((x) => x.key), ["@laws", "fitts-law", "hicks-law", CREATE_SKILL]);
  const none = agentPickerOptions(catalog, { query: " Figma ", entries: [] });
  assert.deepEqual(none.options.map((x) => [x.key, x.label]), [[CREATE_SKILL, "Create skill “Figma”…"]]);
  assert.equal(agentPickerOptions(catalog, { query: "@ux", entries: [] }).options[0].key, "@ux", "a typed @ finds bundles");
});

test("a long list is cut and says how many are not shown; the create option stays last", () => {
  const many = { ...catalog, skills: Array.from({ length: 80 }, (_, i) => ({ name: `s${String(i).padStart(2, "0")}`, description: "" })) };
  const o = agentPickerOptions(many, { query: "", entries: [], limit: 10 });
  assert.equal(o.options.length, 11);
  assert.equal(o.options[10].key, CREATE_SKILL);
  assert.equal(o.hidden, 83 - 10);
});

test("the create links carry the editor to come back to, and the name typed", () => {
  assert.equal(createSkillHref("/agents/ux-designer/edit#skills", ""), "/agents/skills/new?return=%2Fagents%2Fux-designer%2Fedit%23skills");
  assert.equal(createSkillHref("/agents/new", "Figma Handoff"), "/agents/skills/new?name=figma-handoff&return=%2Fagents%2Fnew");
});

test("an agent opens as the catalog read it, a new one empty, and Duplicate starts from a copy", () => {
  assert.deepEqual(agentDraft(catalog, "ux-designer"), {
    previousName: "ux-designer", name: "ux-designer", description: "Designs.", aliases: ["ux"], entries: ["@ux", "tdd"], mcpServers: ["github"], allowedTools: ["Read"],
  });
  assert.deepEqual(agentDraft(catalog, ""), { previousName: "", name: "", description: "", aliases: [], entries: [], mcpServers: [], allowedTools: [] });
  assert.equal(agentDraft(catalog, "nobody"), null);
  const copy = agentDraft(catalog, "", "ux-designer");
  assert.equal(copy.previousName, "");
  assert.equal(copy.name, "");
  assert.deepEqual(copy.entries, ["@ux", "tdd"]);
  assert.equal(copy.entries === catalog.agents[0].entries, false, "a copy, not the catalog's own list");
});

test("the name must be a slug and the description is required; the message is the one the page shows", () => {
  const draft = { ...agentDraft(catalog, "ux-designer"), description: "" };
  const c = checkAgentDraft(draft, { touched: true });
  assert.equal(c.valid, false);
  assert.equal(c.descriptionProblem, "Add a description to save.");
  assert.equal(checkAgentDraft({ ...draft, description: "  " }, { touched: true }).valid, false, "blanks are no description");
  assert.equal(checkAgentDraft({ ...draft, description: "Does it." }).valid, true);
  assert.equal(checkAgentDraft({ ...draft, description: "Does it.", name: "UX Designer" }).nameProblem, "Agent name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
  assert.equal(checkAgentDraft({ ...draft, description: "Does it.", name: "" }).valid, false);
  assert.equal(checkAgentDraft(agentDraft(catalog, ""), { touched: false }).descriptionProblem, "", "a form nobody touched does not scold");
});

test("how many things differ from what was read counts each field once", () => {
  const read = agentDraft(catalog, "ux-designer");
  assert.equal(agentChanges(catalog, read), 0);
  assert.equal(agentChanges(catalog, { ...read, entries: [...read.entries, "lint"], allowedTools: [] }), 2);
  assert.equal(agentChanges(catalog, { ...read, name: "ux", aliases: [] }), 2);
  assert.equal(agentChanges(catalog, agentDraft(catalog, "")), 0);
  assert.equal(agentChanges(catalog, { ...agentDraft(catalog, ""), name: "x", entries: ["tdd"] }), 2);
});

test("MCP servers list with their transport and each variable set or not, and one the catalog lost stays tickable off", () => {
  const rows = mcpChoices(catalog, ["github", "gone"]);
  assert.deepEqual(rows.map((r) => [r.name, r.checked, r.transport, r.missing]), [["github", true, "stdio", false], ["docs", false, "http", false], ["gone", true, "", true]]);
  assert.deepEqual(rows[0].refs, [{ label: "${GITHUB_TOKEN}", state: "not set", tone: "attention" }]);
  assert.equal(rows[1].refs.length, 0);
});

test("allowed tools list the built-in ones with what they do; none ticked means Claude's default tools", () => {
  const t = toolChoices(catalog, ["Read"]);
  assert.deepEqual(t.rows.map((r) => [r.name, r.checked, r.description]), [["Read", true, "Reads a file."], ["Bash", false, "Runs a command."]]);
  assert.equal(t.summary, "1 allowed · everything else is not allowed to this agent");
  assert.equal(toolChoices(catalog, []).summary, "None ticked · sessions get Claude’s default tools");
  assert.deepEqual(toolChoices(catalog, ["Mystery"]).rows.map((r) => r.name), ["Read", "Bash", "Mystery"]);
});

test("the contents list counts what each section holds", () => {
  assert.deepEqual(tocCounts(catalog, { ...agentDraft(catalog, "ux-designer"), allowedTools: [] }), { skills: "4", mcp: "1", tools: "default" });
});

test("a refusal lands on the field it names; unknown names split between skills and servers", () => {
  const draft = { ...agentDraft(catalog, "ux-designer"), entries: ["tdd", "ghost"], mcpServers: ["github", "gone"] };
  const refuse = (code, message, details) => describeRefusal(new StructuredError(code, message, 400, details));
  const unknown = agentFieldErrors(refuse("UNKNOWN_REFERENCE", "unknown", { names: ["ghost", "gone"] }), draft);
  assert.equal(unknown.entries, "Not in the catalog: ghost. Remove it and save again.");
  assert.equal(unknown.mcp, "Not in the catalog: gone. Remove it and save again.");
  assert.equal(agentFieldErrors(refuse("NAME_TAKEN", "x", { suggestion: "ux-designer-2" }), draft).name, "That name is taken. Try ux-designer-2.");
  assert.equal(agentFieldErrors(refuse("INVALID_INPUT", "Description is required", { field: "description" }), draft).description, "Description is required");
  assert.equal(agentFieldErrors(refuse("BUNDLE_CYCLE", "cycle", {}), draft).form, "Bundles cannot include each other, directly or through another bundle.");
  assert.deepEqual(agentFieldErrors(null, draft), { name: "", description: "", aliases: "", entries: "", mcp: "", tools: "", form: "" });
});
