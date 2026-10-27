// The architecture diagram as data: a repository architecture record plus the
// agent catalog become nodes, change edges, guards and regions. Pure, so the
// view only draws what this returns and the Mermaid output reads the same
// model. With no agent focused only change edges are drawn, because read is
// the default for everyone; focus adds that agent's read edges, tools,
// hand-offs and instruction count.

/** The reserved agent name for a session with no agent. */
export const PLAIN = "plain";
/** The pseudo layer for paths that are in no layer. */
export const UNLAYERED = "unlayered";

/** @typedef {"none" | "read" | "change"} Access */

/**
 * @typedef {object} Diagram
 * @property {boolean} draft  Drawn from unsaved changes.
 * @property {string} focus  The focused agent, or "".
 * @property {{ layers: { name: string, pattern: string, overlap: boolean, unmatched: number, files: number | null }[], agents: { name: string, focused: boolean, changeable: number }[] }} nodes
 * @property {{ agent: string, layer: string, access: "change" | "read" }[]} edges
 * @property {{ rule: string, kind: string, mode: "blocking" | "warning", glyph: string, agent: string, layer: string }[]} guards
 * @property {{ name: string, agents: string[] }[]} regions  Sandboxes, with who runs in each.
 * @property {{ name: string, kind: string, writes: boolean }[]} tools  Of the focused agent.
 * @property {{ from: string, to: string, when: string }[]} handoffs  Of the focused agent.
 * @property {{ name: string, kind: string, kindText: string, where: "host" | "sandbox", users: string }[]} toolNodes  Every tool, for the side panel.
 * @property {number} instructionCount  Instruction files that apply to the focused agent.
 */

const TOOL_WORDS = { mcp: "MCP server", function: "function", api: "API", cli: "CLI" };

/** "all agents" or "2 agents": who a tool is for. */
function usersText(usedBy, names) {
  if (!usedBy || usedBy.length === 0 || usedBy.includes("all")) return "all agents";
  const n = names.filter((a) => usedBy.includes(a)).length;
  return `${n} ${n === 1 ? "agent" : "agents"}`;
}

/** The glyph that says a rule's mode without colour. */
export const modeGlyph = (mode) => (mode === "blocking" ? "✕" : "!");

/**
 * @param {object} record  The repository architecture record.
 * @param {{ agents?: string[], focus?: string, draft?: boolean, handoffs?: boolean, problems?: { overlaps?: string[], unmatched?: number }, files?: Record<string, number> | null }} [options]
 *   agents is the catalog's names; handoffs asks for every hand-off, not only the focused agent's;
 *   problems names the layers whose paths overlap and how many files match no layer;
 *   files is the layers' file counts by name from the layer preview (null when it is not known).
 * @returns {Diagram}
 */
export function buildDiagram(record, { agents = [], focus = "", draft = false, handoffs = false, problems = {}, files = null } = {}) {
  const overlapping = problems.overlaps ?? [];
  const unmatched = problems.unmatched ?? 0;
  const layers = record.layers ?? [];
  const names = [...new Set([...agents, PLAIN])];
  const targets = [
    ...layers.map((l) => ({ name: l.name, access: l.access, pattern: patternOf(l), source: sourceOf(l) })),
    { name: UNLAYERED, access: record.unlayered, pattern: "paths in no layer", source: null },
  ];

  const edges = [];
  for (const agent of names) {
    if (focus && agent !== focus) continue;
    for (const t of targets) {
      const access = accessOf(t.access, agent);
      if (access === "change" || (focus && access === "read")) edges.push({ agent, layer: t.name, access });
    }
  }

  const guards = [];
  for (const rule of record.rules ?? []) {
    for (const e of edges) {
      if (e.access !== "change") continue;
      const target = targets.find((t) => t.name === e.layer);
      if (!inScope(rule.agents, e.agent) || !reaches(rule, target)) continue;
      guards.push({ rule: rule.id, kind: rule.kind, mode: rule.mode, glyph: modeGlyph(rule.mode), agent: e.agent, layer: e.layer });
    }
  }

  const fileCount = (name) => {
    if (files === null) return null;
    return name === UNLAYERED ? unmatched : (files[name] ?? null);
  };
  const lists = (xs) => xs ?? [];
  const mine = (xs) => lists(xs).filter((x) => !focus || inScope(x.agents ?? x.used_by ?? x.applies_to, focus));
  return {
    draft,
    focus,
    nodes: {
      layers: targets.map((t) => ({ name: t.name, pattern: t.pattern, overlap: overlapping.includes(t.name), unmatched: t.name === UNLAYERED ? unmatched : 0, files: fileCount(t.name) })),
      agents: names.map((name) => ({ name, focused: name === focus, changeable: layers.filter((l) => accessOf(l.access, name) === "change").length })),
    },
    edges,
    guards,
    regions: lists(record.sandboxes).map((s) => ({ name: s.name, agents: names.filter((a) => inScope(s.used_by, a)) })),
    tools: focus ? mine(lists(record.tools)).map((t) => ({ name: t.name, kind: t.kind, writes: !!t.writes })) : [],
    toolNodes: lists(record.tools).map((t) => ({
      name: t.name,
      kind: t.kind,
      kindText: TOOL_WORDS[t.kind] ?? String(t.kind ?? ""),
      where: t.kind === "function" || t.kind === "cli" ? "sandbox" : "host",
      users: usersText(t.used_by, names),
    })),
    handoffs:
      focus || handoffs
        ? lists(record.orchestration?.handoffs)
            .filter((h) => !focus || h.from === focus || h.to === focus)
            .map((h) => ({ from: h.from, to: h.to, when: h.when }))
        : [],
    instructionCount: focus ? mine(record.instructions).length : 0,
  };
}

/** An agent's access to a layer: its entry, else the layer's default; a layer with no default means read everywhere. @returns {Access} */
export function accessOf(access, agent) {
  return access?.agents?.[agent] ?? access?.default ?? "read";
}

/** Whether a scope list (agent names, "all", or none given) covers an agent. */
function inScope(scope, agent) {
  return !scope || scope.length === 0 || scope.includes("all") || scope.includes(agent);
}

const patternOf = (l) => (l.match?.folder !== undefined ? `${l.match.folder}/` : `/${l.match?.regex ?? ""}/`);
const sourceOf = (l) =>
  l.match?.folder !== undefined ? `${l.match.folder}/` : (l.match?.regex ?? "").replace(/[\^$()|\\]/g, "");

/**
 * Whether a rule can touch paths of a layer. A rule with no paths reaches every
 * layer. One with a regex is tested against the layer's folder, or against its
 * regex stripped of metacharacters; an invalid regex reaches everything.
 */
function reaches(rule, target) {
  if (rule.kind !== "command" || !rule.paths || !target.source) return true;
  try {
    return new RegExp(rule.paths).test(target.source);
  } catch {
    return true;
  }
}
