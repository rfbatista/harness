import { assert, file, test } from "../../../shared/testing/test.js";
import { buildDiagram } from "./diagram.js";
import { agentSlot } from "./layers.js";
import { diagramToSvg, layoutDiagram } from "./svg.js";

file("architecture/domain/svg");

const record = {
  version: 1,
  repository: "harness",
  layers: [
    { name: "domain", match: { folder: "internal/domain" }, access: { default: "read", agents: { "software-architect": "change" } } },
    { name: "adapters", match: { regex: "^internal/adapter/" }, access: { default: "read", agents: { "software-architect": "change", "go-developer": "change" } } },
  ],
  unlayered: { default: "read", agents: { devops: "change" } },
  sandboxes: [{ name: "seatbelt", used_by: ["go-developer", "devops"] }],
  rules: [
    { id: "stay", kind: "edit_outside_layer", mode: "blocking", message: "m" },
    { id: "archtest", kind: "command", at: "before_commit", command: "x", mode: "warning", message: "m" },
  ],
  orchestration: { handoffs: [{ from: "software-architect", to: "go-developer", when: "approved" }] },
};
const AGENTS = ["software-architect", "go-developer", "devops"];
const parse = (svg) => new DOMParser().parseFromString(svg, "image/svg+xml");

test("the SVG is well-formed and draws a node per layer and agent", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS })));
  assert.equal(doc.querySelector("parsererror"), null);
  assert.equal(doc.querySelectorAll("[data-layer]").length, 3); // domain, adapters, unlayered
  assert.equal(doc.querySelectorAll("[data-agent]").length > 3, true); // agents (and guard marks name theirs)
  assert.equal(doc.documentElement.getAttribute("xmlns"), "http://www.w3.org/2000/svg");
});

test("with nothing focused only change edges are drawn, each guard says blocking or warning with a glyph", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS })));
  const edges = [...doc.querySelectorAll("[data-edge]")];
  assert.ok(edges.length > 0);
  assert.ok(edges.every((e) => e.getAttribute("data-access") === "change"));
  const guards = [...doc.querySelectorAll("[data-guard]")];
  assert.ok(guards.length > 0);
  assert.ok(guards.every((g) => g.getAttribute("data-mode") === "blocking" && g.textContent.includes("✕"))); // both rules apply: the blocking one shows
  const warning = parse(diagramToSvg(buildDiagram({ ...record, rules: [record.rules[1]] }, { agents: AGENTS })));
  assert.ok([...warning.querySelectorAll("[data-guard]")].every((g) => g.getAttribute("data-mode") === "warning" && g.textContent.includes("!")));
});

test("a sandbox is a region and a draft is marked", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS, draft: true })));
  assert.equal(doc.querySelectorAll("[data-region=seatbelt]").length, 1);
  assert.equal(doc.querySelectorAll("[data-draft]").length, 1);
  assert.equal(parse(diagramToSvg(buildDiagram(record, { agents: AGENTS }))).querySelectorAll("[data-draft]").length, 0);
});

test("a focused agent adds its read edges and hand-offs and highlights it", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS, focus: "software-architect" })));
  assert.ok([...doc.querySelectorAll("[data-edge]")].every((e) => e.getAttribute("data-edge").startsWith("software-architect>")));
  assert.equal(doc.querySelectorAll("[data-handoff]").length, 1);
  assert.equal(doc.querySelector("g[data-agent][data-focused]").getAttribute("data-agent"), "software-architect");
});

test("names are escaped", () => {
  const svg = diagramToSvg(buildDiagram({ ...record, layers: [{ name: 'a"<b>&', match: { folder: "x" }, access: { default: "read" } }] }, { agents: AGENTS }));
  assert.equal(parse(svg).querySelector("parsererror"), null);
});

test("agents in a sandbox sit together, and no two nodes overlap", () => {
  const L = layoutDiagram(buildDiagram(record, { agents: AGENTS }));
  const boxes = [...L.agents.values()].sort((a, b) => a.y - b.y);
  for (let i = 1; i < boxes.length; i++) assert.ok(boxes[i].y >= boxes[i - 1].y + boxes[i - 1].h);
});

test("each agent drawn is a keyboard-focusable button named for what it does", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS, focus: "devops" })));
  const nodes = [...doc.querySelectorAll("g[data-agent]")];
  assert.equal(nodes.length, 4); // three agents and plain
  for (const g of nodes) {
    assert.equal(g.getAttribute("tabindex"), "0");
    assert.equal(g.getAttribute("role"), "button");
    assert.equal(g.getAttribute("aria-label"), `Focus ${g.getAttribute("data-agent")}`);
  }
  assert.equal(doc.querySelector('g[data-agent="devops"]').getAttribute("aria-pressed"), "true");
});

test("a themed SVG paints by class only, so the page's tokens colour it; the default keeps literal colours for the file", () => {
  const d = buildDiagram(record, { agents: AGENTS, draft: true });
  const themed = diagramToSvg(d, { themed: true });
  assert.equal(/#[0-9a-fA-F]{3,6}"/.test(themed), false, "a literal colour is left in the themed picture");
  const doc = parse(themed);
  assert.equal(doc.querySelector("parsererror"), null);
  assert.ok(doc.querySelector("g[data-layer] rect.dg-layer"));
  assert.ok(doc.querySelector("g[data-agent] rect.dg-agent"));
  assert.ok(doc.querySelector("path[data-edge].dg-edge-change"));
  assert.ok(doc.querySelector("g[data-guard][data-mode=blocking] circle.dg-guard-block"));
  const file = diagramToSvg(d);
  assert.ok(file.includes("#2563eb") && file.includes('fill="#ffffff"'));
});

test("a layer with overlapping paths is marked, and the unlayered node says how many files match no layer", () => {
  const d = buildDiagram(record, { agents: AGENTS, problems: { overlaps: ["domain"], unmatched: 12 } });
  const doc = parse(diagramToSvg(d));
  assert.equal(doc.querySelectorAll("g[data-layer][data-overlap]").length, 1);
  assert.equal(doc.querySelector("g[data-layer][data-overlap]").getAttribute("data-layer"), "domain");
  assert.ok(doc.querySelector("g[data-layer=unlayered]").textContent.includes("12 files match no layer"));
});

test("hand-offs asked for are drawn with nobody focused", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS, handoffs: true })));
  assert.equal(doc.querySelectorAll("[data-handoff]").length, 1);
});

const crossing = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
const manyBoxes = {
  ...record,
  sandboxes: [
    { name: "seatbelt", used_by: ["go-developer", "devops"] },
    { name: "docker", used_by: ["software-architect", "devops"] },
    { name: "vm", used_by: ["go-developer"] },
  ],
};

test("sandbox regions never overlap and a label is never covered, however the agents are shared between sandboxes", () => {
  const L = layoutDiagram(buildDiagram(manyBoxes, { agents: AGENTS }));
  const regions = [...L.regions.values()];
  assert.equal(regions.length, 2); // vm has no agent of its own: every one is in an earlier sandbox
  for (let i = 0; i < regions.length; i++) {
    for (let j = i + 1; j < regions.length; j++) assert.equal(crossing(regions[i], regions[j]), false, "two regions overlap");
  }
  for (const r of regions) {
    for (const [name, box] of L.agents) assert.equal(crossing(r.label, box), false, `the label of ${r.name} is under ${name}`);
    assert.ok(r.label.x >= r.x && r.label.y >= r.y && r.label.x + r.label.w <= r.x + r.w, "the label is outside its region");
  }
});

test("an agent in several sandboxes sits in the first and names the others in its subtitle", () => {
  const d = buildDiagram(manyBoxes, { agents: AGENTS });
  const L = layoutDiagram(d);
  const inside = (agent, region) => crossing(L.regions.get(region), L.agents.get(agent)) && L.regions.get(region).y <= L.agents.get(agent).y;
  assert.ok(inside("go-developer", "seatbelt") && inside("devops", "seatbelt") && inside("software-architect", "docker"));
  const doc = parse(diagramToSvg(d));
  const sub = (a) => doc.querySelector(`g[data-agent="${a}"] .dg-sub`).textContent;
  assert.equal(sub("devops"), "0 layers changeable · also in docker");
  assert.equal(sub("go-developer"), "1 layer changeable · also in vm");
  assert.equal(sub("software-architect"), "2 layers changeable");
});

test("one sandbox holding every agent frames the agents and the layers together", () => {
  const d = buildDiagram({ ...record, sandboxes: [{ name: "seatbelt", used_by: ["all"] }] }, { agents: AGENTS });
  const L = layoutDiagram(d);
  assert.equal(L.regions.size, 1);
  const r = [...L.regions.values()][0];
  for (const b of [...L.agents.values(), ...L.layers.values()]) assert.ok(b.x >= r.x && b.x + b.w <= r.x + r.w && b.y >= r.y && b.y + b.h <= r.y + r.h);
});

test("an agent box is left-aligned, carries the agent's colour on its left edge and says how many layers it may change", () => {
  const doc = parse(diagramToSvg(buildDiagram(record, { agents: AGENTS })));
  const g = doc.querySelector('g[data-agent="go-developer"]');
  assert.equal(g.querySelector("text").getAttribute("text-anchor"), null);
  assert.equal(g.querySelector("rect.dg-agent-edge").getAttribute("data-slot"), String(agentSlot("go-developer")));
  assert.equal(g.querySelector(".dg-sub").textContent.startsWith("1 layer changeable"), true);
  const themed = diagramToSvg(buildDiagram(record, { agents: AGENTS }), { themed: true });
  assert.equal(/#[0-9a-fA-F]{3,6}"/.test(themed), false);
});

test("a layer box shows its name, its pattern and, when the preview is known, its file count on the right", () => {
  const d = buildDiagram(record, { agents: AGENTS, files: { domain: 84, adapters: 176 }, problems: { unmatched: 3 } });
  const doc = parse(diagramToSvg(d));
  const n = doc.querySelector('g[data-layer="domain"] [data-files]');
  assert.equal(n.textContent, "84 files");
  assert.equal(n.getAttribute("text-anchor"), "end");
  assert.equal(doc.querySelector('g[data-layer="unlayered"] [data-files]').textContent, "3 files");
  assert.equal(parse(diagramToSvg(buildDiagram(record, { agents: AGENTS }))).querySelectorAll("[data-files]").length, 0);
});

test("each edge has one guard, drawn at the layer's end: the blocking one when a blocking and a warning rule both apply", () => {
  const d = buildDiagram(record, { agents: AGENTS });
  const doc = parse(diagramToSvg(d));
  const edges = doc.querySelectorAll("[data-edge]").length;
  const guards = [...doc.querySelectorAll("g[data-guard]")];
  assert.equal(guards.length, edges); // the record has a blocking and a warning rule on every change edge
  assert.ok(guards.every((g) => g.getAttribute("data-mode") === "blocking" && g.textContent.includes("✕")));
  const L = layoutDiagram(d);
  for (const g of guards) {
    const layer = L.layers.get(g.getAttribute("data-guard-layer"));
    const cx = Number(g.querySelector("circle").getAttribute("cx"));
    assert.ok(cx < layer.x && cx > layer.x - 30, "the guard is not at the layer end");
  }
  const only = { ...record, rules: [record.rules[1]] };
  const warn = parse(diagramToSvg(buildDiagram(only, { agents: AGENTS })));
  assert.ok([...warn.querySelectorAll("g[data-guard]")].every((g) => g.getAttribute("data-mode") === "warning"));
});

test("tools are drawn as nodes in a Harness host box and a Tools inside the sandbox box, with no edges unless an agent is focused", () => {
  const rec = { ...record, tools: [{ name: "task", kind: "mcp", used_by: ["all"] }, { name: "make check", kind: "function", used_by: ["go-developer"] }] };
  const doc = parse(diagramToSvg(buildDiagram(rec, { agents: AGENTS })));
  assert.equal(doc.querySelector('[data-tool-region=host] ~ g[data-tool="task"], g[data-tool="task"]').textContent.includes("MCP server · all agents"), true);
  assert.ok(doc.querySelector("g[data-tool-region=host]").textContent.includes("Harness host"));
  assert.ok(doc.querySelector("g[data-tool-region=sandbox]").textContent.includes("Tools inside the sandbox"));
  assert.equal(doc.querySelectorAll("[data-tool-edge]").length, 0);
  const focused = parse(diagramToSvg(buildDiagram(rec, { agents: AGENTS, focus: "go-developer" })));
  assert.equal(focused.querySelectorAll("[data-tool-edge]").length, 2);
  assert.equal(parse(diagramToSvg(buildDiagram(record, { agents: AGENTS }))).querySelector("[data-tool-region]"), null);
});
