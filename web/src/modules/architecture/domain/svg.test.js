import { assert, file, test } from "../../../shared/testing/test.js";
import { buildDiagram } from "./diagram.js";
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
  const blocking = guards.filter((g) => g.getAttribute("data-mode") === "blocking");
  const warning = guards.filter((g) => g.getAttribute("data-mode") === "warning");
  assert.ok(blocking.length > 0 && warning.length > 0);
  assert.ok(blocking.every((g) => g.textContent.includes("✕")));
  assert.ok(warning.every((g) => g.textContent.includes("!")));
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
