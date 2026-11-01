import { assert, file, test } from "../../../shared/testing/test.js";
import { applyProposal, proposalConflicts, groupChanges } from "./proposal.js";

file("architecture/domain/proposal");

const record = () => ({
  version: 1,
  pattern: "layered",
  layers: [
    { name: "domain", match: { folder: "internal/domain" }, purpose: "core", access: { default: "read", agents: { "go-developer": "change" } } },
    { name: "web", match: { folder: "web" }, access: { default: "read" } },
  ],
  unlayered: { default: "read", agents: { devops: "change" } },
  rules: [{ id: "stay", kind: "edit_outside_layer", mode: "warning", message: "m" }],
  instructions: [{ path: "CLAUDE.md", kind: "rules" }],
  tools: [{ name: "github", kind: "mcp" }],
  sandboxes: [{ name: "seatbelt" }],
  orchestration: { routing: [{ agent: "go-developer", model: "sonnet" }], handoffs: [{ from: "a", to: "b", when: "w" }] },
});

const proposal = (...changes) => ({ id: "p1", summary: "s", valid: true, errors: [], changes });
const ch = (target, op, value, base = null) => ({ target, op, value, base, reason: "r" });

test("applying never changes the record it was given", () => {
  const r = record();
  applyProposal(r, proposal(ch({ kind: "pattern" }, "change", "hexagonal")));
  assert.deepEqual(r, record());
});

test("the pattern is set by change and cleared by remove", () => {
  let r = applyProposal(record(), proposal(ch({ kind: "pattern" }, "change", "hexagonal")));
  assert.equal(r.pattern, "hexagonal");
  r = applyProposal(r, proposal(ch({ kind: "pattern" }, "remove")));
  assert.equal("pattern" in r, false);
});

test("a new layer is appended with its access; a changed layer keeps its access", () => {
  let r = applyProposal(
    record(),
    proposal(ch({ kind: "layer", name: "adapters" }, "add", { name: "adapters", match: { regex: "^internal/adapter" }, access: { default: "none" } })),
  );
  assert.deepEqual(r.layers.map((l) => l.name), ["domain", "web", "adapters"]);
  assert.deepEqual(r.layers[2].access, { default: "none" });
  r = applyProposal(r, proposal(ch({ kind: "layer", name: "domain" }, "change", { name: "domain", match: { folder: "core" } })));
  assert.deepEqual(r.layers[0], { name: "domain", match: { folder: "core" }, access: { default: "read", agents: { "go-developer": "change" } } });
});

test("a layer is removed with its access", () => {
  const r = applyProposal(record(), proposal(ch({ kind: "layer", name: "web" }, "remove")));
  assert.deepEqual(r.layers.map((l) => l.name), ["domain"]);
});

test("layer access sets and drops an agent's entry, and * is the default", () => {
  let r = applyProposal(record(), proposal(ch({ kind: "layer_access", layer: "web", agent: "devops" }, "add", "change")));
  assert.deepEqual(r.layers[1].access, { default: "read", agents: { devops: "change" } });
  r = applyProposal(r, proposal(ch({ kind: "layer_access", layer: "web", agent: "devops" }, "remove")));
  assert.deepEqual(r.layers[1].access, { default: "read", agents: {} });
  r = applyProposal(r, proposal(ch({ kind: "layer_access", layer: "web", agent: "*" }, "change", "none")));
  assert.equal(r.layers[1].access.default, "none");
});

test("unlayered access follows the same rules", () => {
  let r = applyProposal(record(), proposal(ch({ kind: "unlayered_access", agent: "*" }, "change", "none")));
  assert.equal(r.unlayered.default, "none");
  r = applyProposal(r, proposal(ch({ kind: "unlayered_access", agent: "devops" }, "remove")));
  assert.deepEqual(r.unlayered.agents, {});
});

test("rules, instructions, tools, sandboxes, routes and hand-offs are added, changed and removed by their key", () => {
  const rule = { id: "no-cmd", kind: "command", at: "before_commit", mode: "warning", message: "x", command: "make" };
  let r = applyProposal(record(), proposal(ch({ kind: "rule", id: "no-cmd" }, "add", rule)));
  assert.deepEqual(r.rules.map((x) => x.id), ["stay", "no-cmd"]);
  r = applyProposal(r, proposal(ch({ kind: "rule", id: "stay" }, "change", { ...record().rules[0], mode: "blocking" })));
  assert.equal(r.rules[0].mode, "blocking");
  r = applyProposal(r, proposal(ch({ kind: "rule", id: "no-cmd" }, "remove")));
  assert.equal(r.rules.length, 1);

  r = applyProposal(r, proposal(ch({ kind: "instruction", path: "AGENTS.md" }, "add", { path: "AGENTS.md", kind: "rules" })));
  r = applyProposal(r, proposal(ch({ kind: "tool", name: "github" }, "change", { name: "github", kind: "mcp", writes: false })));
  r = applyProposal(r, proposal(ch({ kind: "sandbox", name: "seatbelt" }, "remove")));
  r = applyProposal(r, proposal(ch({ kind: "routing", agent: "go-developer" }, "change", { agent: "go-developer", model: "opus" })));
  r = applyProposal(r, proposal(ch({ kind: "handoff", from: "a", to: "b" }, "remove")));
  assert.deepEqual(r.instructions.map((x) => x.path), ["CLAUDE.md", "AGENTS.md"]);
  assert.equal(r.tools[0].writes, false);
  assert.deepEqual(r.sandboxes, []);
  assert.equal(r.orchestration.routing[0].model, "opus");
  assert.deepEqual(r.orchestration.handoffs, []);
});

test("an add of an item that already exists replaces it in place", () => {
  const r = applyProposal(record(), proposal(ch({ kind: "tool", name: "github" }, "add", { name: "github", kind: "api" })));
  assert.deepEqual(r.tools, [{ name: "github", kind: "api" }]);
});

test("changes apply in order", () => {
  const r = applyProposal(
    record(),
    proposal(
      ch({ kind: "layer", name: "adapters" }, "add", { name: "adapters", match: { folder: "a" }, access: { default: "read" } }),
      ch({ kind: "layer_access", layer: "adapters", agent: "go-developer" }, "add", "change"),
    ),
  );
  assert.deepEqual(r.layers[2].access, { default: "read", agents: { "go-developer": "change" } });
});

test("a change to something the person left alone is no conflict", () => {
  const r = record();
  const p = proposal(ch({ kind: "rule", id: "stay" }, "change", { ...r.rules[0], mode: "blocking" }, r.rules[0]));
  assert.deepEqual(proposalConflicts(r, r, p), []);
});

test("an item edited after the turn is a conflict with both values", () => {
  const saved = record();
  const draft = record();
  draft.rules[0].mode = "blocking";
  draft.rules[0].message = "mine";
  const p = proposal(ch({ kind: "rule", id: "stay" }, "change", { ...saved.rules[0], message: "theirs" }, saved.rules[0]));
  const [c] = proposalConflicts(draft, saved, p);
  assert.equal(c.index, 0);
  assert.deepEqual(c.draftValue, draft.rules[0]);
  assert.deepEqual(c.proposedValue, { ...saved.rules[0], message: "theirs" });
});

test("an item the person edited before the turn still differs from the saved record", () => {
  const saved = record();
  const draft = record();
  draft.tools[0].writes = true;
  const p = proposal(ch({ kind: "tool", name: "github" }, "change", { name: "github", kind: "mcp", writes: false }, draft.tools[0]));
  assert.equal(proposalConflicts(draft, saved, p).length, 1);
});

test("a layer change compares the layer without its access", () => {
  const saved = record();
  const draft = record();
  draft.layers[0].access.default = "none";
  const p = proposal(ch({ kind: "layer", name: "domain" }, "change", { name: "domain", match: { folder: "core" }, purpose: "core" }, saved.layers[0]));
  assert.deepEqual(proposalConflicts(draft, saved, p), []);
});

test("an added item the person already added is a conflict unless it is the same", () => {
  const saved = record();
  const draft = record();
  draft.tools.push({ name: "pg", kind: "cli" });
  const same = proposal(ch({ kind: "tool", name: "pg" }, "add", { name: "pg", kind: "cli" }));
  const other = proposal(ch({ kind: "tool", name: "pg" }, "add", { name: "pg", kind: "api" }));
  assert.deepEqual(proposalConflicts(draft, saved, same), []);
  assert.equal(proposalConflicts(draft, saved, other).length, 1);
});

test("an access cell and the * default cell are conflicts of their own", () => {
  const saved = record();
  const draft = record();
  draft.layers[0].access.agents["go-developer"] = "read";
  draft.unlayered.default = "none";
  const p = proposal(
    ch({ kind: "layer_access", layer: "domain", agent: "go-developer" }, "change", "none", "change"),
    ch({ kind: "unlayered_access", agent: "*" }, "change", "change", "read"),
    ch({ kind: "layer_access", layer: "web", agent: "*" }, "change", "none", "read"),
  );
  assert.deepEqual(proposalConflicts(draft, saved, p).map((c) => c.index), [0, 1]);
});

test("changes group by the tab they touch, in the tab order, keeping their own order inside", () => {
  const p = proposal(
    ch({ kind: "layer", name: "a" }, "add", { name: "a" }),
    ch({ kind: "tool", name: "t" }, "add", { name: "t" }),
    ch({ kind: "layer_access", layer: "a", agent: "*" }, "change", "none"),
    ch({ kind: "pattern" }, "change", "hex"),
    ch({ kind: "rule", id: "r" }, "add", { id: "r" }),
  );
  const groups = groupChanges(p);
  assert.deepEqual(groups.map((g) => g.label), ["Pattern", "Tools", "Hooks", "Layers"]);
  assert.equal(groups[3].changes.length, 2);
});
