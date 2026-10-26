import { assert, file, test } from "../../../shared/testing/test.js";
import { changeSummary, exportBadge, statCards, tabCounts } from "./record.js";
import { discoveryText } from "./descriptive.js";
import { hexagonalLayers } from "./layers.js";
import { EVENTS_COMPACT, shownEvents } from "./events.js";

file("architecture/domain/overview");

const record = {
  instructions: [{ path: "AGENTS.md" }, { path: "CLAUDE.md" }],
  tools: [{ name: "codegraph" }],
  sandboxes: [{ name: "seatbelt" }, { name: "docker" }, { name: "vm" }],
  orchestration: {
    routing: [{ agent: "go-developer" }, { agent: "plain" }, { agent: "go-developer" }],
    handoffs: [{ from: "a", to: "b" }],
  },
  rules: [{ mode: "blocking" }, { mode: "warning" }, { mode: "blocking" }],
  layers: [{ name: "domain" }, { name: "web" }],
};

test("the stat cards read the shape of the record: routed agents are distinct agents, overlaps come from the preview", () => {
  const cards = statCards(record, 1);
  assert.deepEqual(
    cards.map((c) => [c.key, c.value, c.label]),
    [
      ["instructions", 2, "instruction files"],
      ["tools", 1, "tools"],
      ["sandboxes", 3, "sandboxes"],
      ["agents", 2, "routed agents"],
      ["hooks", 3, "hook rules · 2 blocking"],
      ["layers", 2, "layers · 1 overlap"],
    ],
  );
});

test("overlaps are counted in the plural and an empty record has zeros", () => {
  assert.equal(statCards(record, 3)[5].label, "layers · 3 overlaps");
  assert.equal(statCards(record, 0)[5].label, "layers · 0 overlaps");
  assert.deepEqual(statCards(undefined, 0).map((c) => c.value), [0, 0, 0, 0, 0, 0]);
});

test("each tab counts its entries; Orchestration counts routes and hand-offs, Output has none", () => {
  assert.deepEqual(tabCounts(record), {
    instructions: 2, tools: 1, sandboxes: 3, orchestration: 4, hooks: 3, layers: 2, output: null,
  });
});

test("the sync pill names the file the record is in sync with", () => {
  const pill = exportBadge({ state: "in_sync", path: ".harness/architecture.yaml" });
  assert.equal(pill.label, "In sync with .harness/architecture.yaml");
});

test("the save bar sums up the changes by tab", () => {
  const rows = [
    { pointer: "/tools/0/usage" },
    { pointer: "/tools/1/name" },
    { pointer: "/layers/0/name" },
  ];
  assert.equal(changeSummary(rows), "3 changes: 2 in Tools, 1 in Layers");
  assert.equal(changeSummary([rows[0]]), "1 change: 1 in Tools");
  assert.equal(changeSummary([]), "0 changes");
});

test("the events panel shows five rows until Show all", () => {
  const events = Array.from({ length: 12 }, (_, i) => ({ id: `e${i}` }));
  assert.equal(EVENTS_COMPACT, 5);
  assert.equal(shownEvents(events, false).length, 5);
  assert.equal(shownEvents(events, true).length, 12);
  assert.equal(shownEvents(events.slice(0, 3), false).length, 3);
});

test("the discovery banner names rule files and counts skills, sub-agent prompts, tools and sandboxes", () => {
  const c = (key, kind, label) => ({ key, label, entry: { kind } });
  const text = discoveryText([
    c("instructions:0", "rules", "AGENTS.md"),
    c("instructions:1", "rules", "CLAUDE.md"),
    c("instructions:2", "skill", "a/SKILL.md"),
    c("instructions:3", "skill", "b/SKILL.md"),
    c("instructions:4", "subagent_prompt", "x.md"),
    c("tools:0", "mcp", "codegraph"),
    c("sandboxes:0", undefined, "seatbelt"),
  ]);
  assert.equal(
    text,
    "We found AGENTS.md, CLAUDE.md, 2 skills, 1 sub-agent prompt, 1 tool and 1 sandbox in the repository. Confirm them to add them to the architecture.",
  );
  assert.equal(discoveryText([]), "");
});

test("the hexagonal preset has eight layers, each with the default access", () => {
  const layers = hexagonalLayers();
  assert.deepEqual(layers.map((l) => l.name), ["domain", "ports", "application", "adapters-in", "adapters-out", "web", "tests", "docs"]);
  for (const l of layers) {
    assert.deepEqual(l.access, { default: "read", agents: { "software-architect": "change" } });
  }
  assert.equal(layers[0].match.folder, "internal/domain");
  assert.equal(typeof layers[6].match.regex, "string");
});
