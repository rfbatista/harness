import { assert, file, test } from "../../../shared/testing/test.js";
import { buildDiagram } from "./diagram.js";
import { diagramToMermaid } from "./mermaid.js";

file("architecture/domain/diagram");

const AGENTS = ["software-architect", "go-developer", "devops"];

const record = () => ({
  version: 1,
  repository: "harness",
  layers: [
    {
      name: "domain",
      match: { folder: "internal/domain" },
      access: { default: "read", agents: { "software-architect": "change", "go-developer": "none" } },
    },
    {
      name: "adapters",
      match: { regex: "^internal/adapter/" },
      access: { default: "read", agents: { "software-architect": "change", "go-developer": "change" } },
    },
  ],
  unlayered: { default: "read", agents: { "software-architect": "change", devops: "change" } },
  rules: [
    { id: "stay-in-your-layer", kind: "edit_outside_layer", mode: "blocking", message: "m" },
    {
      id: "archtest",
      kind: "command",
      at: "before_commit",
      command: "go test ./...",
      paths: "^internal/domain",
      mode: "warning",
      message: "m",
      agents: ["go-developer"],
    },
  ],
  sandboxes: [{ name: "seatbelt", runs: "Every session.", can_reach: [], cannot_reach: [], used_by: ["all"] }],
  tools: [
    { name: "codegraph", kind: "mcp", used_by: ["all"], writes: false },
    { name: "deploy", kind: "api", used_by: ["devops"], writes: true, usage: "u" },
  ],
  instructions: [
    { path: "CLAUDE.md", kind: "rules", applies_to: ["all"], loaded: "always" },
    { path: "go.md", kind: "skill", applies_to: ["go-developer"], loaded: "on_demand" },
  ],
  orchestration: {
    routing: [],
    handoffs: [
      { from: "software-architect", to: "go-developer", when: "A spec is approved." },
      { from: "devops", to: "software-architect", when: "x" },
    ],
  },
});

const edgeKeys = (m) => m.edges.map((e) => `${e.agent}>${e.layer}:${e.access}`).sort();

test("with no agent focused only change edges are drawn, for every agent, plain and unlayered included", () => {
  const m = buildDiagram(record(), { agents: AGENTS });
  assert.deepEqual(edgeKeys(m), [
    "devops>unlayered:change",
    "go-developer>adapters:change",
    "software-architect>adapters:change",
    "software-architect>domain:change",
    "software-architect>unlayered:change",
  ]);
  assert.deepEqual(m.nodes.layers.map((l) => l.name), ["domain", "adapters", "unlayered"]);
  assert.deepEqual(m.nodes.agents.map((a) => a.name), [...AGENTS, "plain"]);
});

test("an agent not listed on a layer gets the layer's default", () => {
  const r = record();
  r.layers[0].access.default = "change";
  const m = buildDiagram(r, { agents: AGENTS });
  assert.ok(edgeKeys(m).includes("devops>domain:change"));
  assert.ok(edgeKeys(m).includes("plain>domain:change"));
  assert.ok(!edgeKeys(m).includes("go-developer>domain:change"), "an explicit none beats the default");
});

test("focusing an agent shows only its change and read edges", () => {
  const m = buildDiagram(record(), { agents: AGENTS, focus: "go-developer" });
  assert.deepEqual(edgeKeys(m), ["go-developer>adapters:change", "go-developer>unlayered:read"]);
  assert.equal(m.focus, "go-developer");
  assert.ok(m.nodes.agents.find((a) => a.name === "go-developer").focused);
});

test("a focused agent brings its tools, hand-offs and instruction count", () => {
  const m = buildDiagram(record(), { agents: AGENTS, focus: "go-developer" });
  assert.deepEqual(m.tools.map((t) => t.name), ["codegraph"]);
  assert.deepEqual(m.handoffs.map((h) => `${h.from}>${h.to}`), ["software-architect>go-developer"]);
  assert.equal(m.instructionCount, 2);
  const none = buildDiagram(record(), { agents: AGENTS });
  assert.deepEqual([none.tools, none.handoffs], [[], []], "tools and hand-offs only on request");
});

test("rules sit on the change edges they apply to, blocking and warning apart", () => {
  const m = buildDiagram(record(), { agents: AGENTS });
  const on = (agent, layer) => m.guards.filter((g) => g.agent === agent && g.layer === layer).map((g) => `${g.rule}:${g.mode}:${g.glyph}`);
  assert.deepEqual(on("software-architect", "domain"), ["stay-in-your-layer:blocking:✕"]);
  assert.deepEqual(on("go-developer", "adapters"), ["stay-in-your-layer:blocking:✕"], "paths ^internal/domain does not reach adapters");
  const r = record();
  r.rules[1].paths = "^internal/adapter";
  const m2 = buildDiagram(r, { agents: AGENTS });
  const g = m2.guards.filter((x) => x.agent === "go-developer" && x.layer === "adapters").map((x) => `${x.rule}:${x.glyph}`);
  assert.deepEqual(g, ["stay-in-your-layer:✕", "archtest:!"]);
});

test("a rule scoped to some agents leaves the others' edges unguarded", () => {
  const r = record();
  r.rules[0].agents = ["devops"];
  const m = buildDiagram(r, { agents: AGENTS });
  assert.deepEqual(m.guards.map((g) => `${g.agent}>${g.layer}`), ["devops>unlayered"]);
});

test("a sandbox is a region around the agents that use it", () => {
  const r = record();
  r.sandboxes.push({ name: "vm", runs: "r", can_reach: [], cannot_reach: [], used_by: ["devops", "plain"] });
  const m = buildDiagram(r, { agents: AGENTS });
  assert.deepEqual(m.regions.map((x) => [x.name, x.agents]), [
    ["seatbelt", [...AGENTS, "plain"]],
    ["vm", ["devops", "plain"]],
  ]);
});

test("the draft flag is carried into the model", () => {
  assert.equal(buildDiagram(record(), { agents: AGENTS }).draft, false);
  assert.equal(buildDiagram(record(), { agents: AGENTS, draft: true }).draft, true);
});

test("an empty record draws nothing but the unlayered node and the agents", () => {
  const m = buildDiagram({ layers: [], rules: [], sandboxes: [], tools: [], instructions: [], orchestration: { routing: [], handoffs: [] }, unlayered: { default: "read", agents: {} } }, { agents: AGENTS });
  assert.deepEqual(m.edges, []);
  assert.equal(m.nodes.layers.length, 1);
});

test("Mermaid output names every layer, agent, guard and region, and is stable", () => {
  const m = buildDiagram(record(), { agents: AGENTS });
  const text = diagramToMermaid(m);
  assert.equal(text, diagramToMermaid(buildDiagram(record(), { agents: AGENTS })));
  assert.ok(text.startsWith("flowchart LR"));
  for (const name of ["domain", "adapters", "unlayered", "software-architect", "go-developer", "devops", "plain", "seatbelt"]) {
    assert.ok(text.includes(`"${name}"`) || text.includes(`["${name}"]`) || text.includes(name), name);
  }
  assert.ok(text.includes("subgraph"));
  assert.ok(text.includes("✕ stay-in-your-layer"));
  assert.ok(!text.includes("Draft"));
});

test("Mermaid marks a draft and escapes names that would break the syntax", () => {
  const r = record();
  r.layers[0].name = 'dom"ain [x]';
  r.layers[0].access.agents["software-architect"] = "change";
  const m = buildDiagram(r, { agents: AGENTS, draft: true });
  const text = diagramToMermaid(m);
  assert.ok(text.includes("%% Draft: unsaved changes"));
  assert.ok(!text.includes('dom"ain'), "a quote must not survive raw");
  assert.ok(text.includes("dom#quot;ain"));
});

test("a layer with no default access is read everywhere, so only an explicit change draws an edge", () => {
  const r = { layers: [{ name: "docs", match: { folder: "docs" }, access: { agents: { devops: "change" } } }], unlayered: { default: "none" } };
  const d = buildDiagram(r, { agents: AGENTS, focus: "go-developer" });
  assert.deepEqual(d.edges, [{ agent: "go-developer", layer: "docs", access: "read" }]);
  const all = buildDiagram(r, { agents: AGENTS });
  assert.deepEqual(all.edges, [{ agent: "devops", layer: "docs", access: "change" }]);
});

test("hand-offs of everyone are in the model when asked for, not only the focused agent's", () => {
  assert.equal(buildDiagram(record(), { agents: AGENTS }).handoffs.length, 0);
  const all = buildDiagram({ ...record(), orchestration: { handoffs: [{ from: "devops", to: "go-developer", when: "w" }, { from: "software-architect", to: "devops", when: "w" }] } }, { agents: AGENTS, handoffs: true });
  assert.equal(all.handoffs.length, 2);
});

test("layers with overlapping paths and the count of unmatched files are marked on the model", () => {
  const d = buildDiagram(record(), { agents: AGENTS, problems: { overlaps: ["domain"], unmatched: 12 } });
  assert.equal(d.nodes.layers.find((l) => l.name === "domain").overlap, true);
  assert.equal(d.nodes.layers.find((l) => l.name === "adapters").overlap, false);
  assert.equal(d.nodes.layers.find((l) => l.name === "unlayered").unmatched, 12);
  assert.equal(buildDiagram(record(), { agents: AGENTS }).nodes.layers.every((l) => !l.overlap && !l.unmatched), true);
});

test("each agent counts the layers it may change, and a layer carries its file count when the preview is known", () => {
  const d = buildDiagram(record(), { agents: AGENTS, files: { domain: 84, adapters: 176 }, problems: { unmatched: 12 } });
  const byName = Object.fromEntries(d.nodes.agents.map((a) => [a.name, a.changeable]));
  assert.deepEqual(byName, { "software-architect": 2, "go-developer": 1, devops: 0, plain: 0 }); // unlayered paths are not a layer
  assert.deepEqual(d.nodes.layers.map((l) => l.files), [84, 176, 12]);
  assert.deepEqual(buildDiagram(record(), { agents: AGENTS }).nodes.layers.map((l) => l.files), [null, null, null]);
});

test("tools are listed for the side panel: MCP servers and APIs on the host, functions and CLIs in the sandbox, each with who uses it", () => {
  const rec = {
    ...record(),
    tools: [
      { name: "task", kind: "mcp", used_by: ["all"] },
      { name: "github", kind: "mcp", used_by: ["devops", "go-developer"] },
      { name: "make check", kind: "function", used_by: ["go-developer"] },
      { name: "gh", kind: "cli" },
    ],
  };
  const d = buildDiagram(rec, { agents: AGENTS });
  assert.deepEqual(
    d.toolNodes.map((t) => [t.name, t.where, t.users]),
    [["task", "host", "all agents"], ["github", "host", "2 agents"], ["make check", "sandbox", "1 agent"], ["gh", "sandbox", "all agents"]],
  );
  assert.deepEqual(d.toolNodes.map((t) => t.kindText), ["MCP server", "MCP server", "function", "CLI"]);
  assert.equal(d.tools.length, 0); // the focused agent's list is still only for a focus
});
