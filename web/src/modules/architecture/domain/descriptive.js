// The descriptive parts of the record (instructions, tools, sandboxes,
// orchestration): blank entries, list editing by path, and what discovery
// adds. Pure. A "path" is the keys from the record to a list, e.g.
// ["orchestration", "routing"].

export const INSTRUCTION_KINDS = Object.freeze(["rules", "skill", "subagent_prompt"]);
export const LOADED = Object.freeze(["always", "on_demand"]);
export const TOOL_KINDS = Object.freeze(["mcp", "function", "api", "cli"]);

export const blankInstruction = () => ({ path: "", kind: "rules", applies_to: ["all"], loaded: "always" });
export const blankTool = () => ({ name: "", kind: "mcp", used_by: ["all"], writes: false });
export const blankSandbox = () => ({ name: "", runs: "", can_reach: [], cannot_reach: [], used_by: ["all"] });
export const blankRoute = () => ({ agent: "", model: "" });
export const blankHandoff = () => ({ from: "", to: "", when: "" });

const KIND_LABELS = {
  instruction: { rules: "Rules", skill: "Skill", subagent_prompt: "Sub-agent prompt" },
  tool: { mcp: "MCP server", function: "Function", api: "API", cli: "CLI" },
};

/** What a row's kind tag says: "subagent_prompt" as "Sub-agent prompt". @param {"instruction" | "tool"} of */
export const kindLabel = (of, kind) => KIND_LABELS[of]?.[kind] ?? String(kind ?? "");

/** What a row says about when an instruction file loads. */
export const loadedLabel = (loaded) => (loaded === "on_demand" ? "On demand" : loaded === "always" ? "Always" : String(loaded ?? ""));

/** Whether a tool that writes has no usage prose (the record refuses it). */
export const noUsage = (tool) => tool?.writes === true && String(tool?.usage ?? "").trim() === "";

/** The line under a tool: its usage prose, or what its absence means to the model. */
export const usageText = (tool) =>
  String(tool?.usage ?? "").trim() === "" ? "The model gets no guidance on when to call this." : tool.usage;

/** Whether a sandbox's list (can_reach, cannot_reach) has anything to show. */
export const reaches = (sandbox, field) => Array.isArray(sandbox?.[field]) && sandbox[field].length > 0;

/** "a, b" as ["a", "b"]; blanks dropped. */
export const fromCsv = (text) =>
  String(text ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "");

/** ["a", "b"] as "a, b". */
export const toCsv = (list) => (Array.isArray(list) ? list.join(", ") : "");

/** The list at `path` in the record, or an empty one. */
export function rowsAt(record, path) {
  let v = record;
  for (const k of path) v = v?.[k];
  return Array.isArray(v) ? v : [];
}

/** Appends `row` to the list at `path`, making the list if the record has none. */
export function addRow(record, path, row) {
  let v = record;
  for (const k of path.slice(0, -1)) v = v[k] ??= {};
  const key = path[path.length - 1];
  (v[key] ??= []).push(row);
}

export function removeRow(record, path, index) {
  rowsAt(record, path).splice(index, 1);
}

/** The JSON pointer of a field of a row: (["tools"], 2, "usage") is "/tools/2/usage". */
export const pointer = (path, index, field) => `/${[...path, index, field].join("/")}`;

/** Sets a list field of a row from comma-separated text. */
export function setList(row, field, text) {
  row[field] = fromCsv(text);
}

/**
 * Discovery's candidates as a flat list to confirm, all ticked.
 * @returns {{ key: string, section: string, label: string, detail: string, on: boolean, entry: any }[]}
 */
export function candidatesOf(found) {
  const out = [];
  const list = (xs) => (Array.isArray(xs) ? xs : []);
  list(found?.instructions).forEach((i, n) =>
    out.push({
      key: `instructions:${n}`,
      section: "Instruction",
      label: i.path,
      detail: `${i.kind ?? "rules"} · loaded ${(i.loaded ?? "always").replace("_", " ")}`,
      on: true,
      entry: { path: i.path, kind: i.kind ?? "rules", applies_to: ["all"], loaded: i.loaded ?? "always" },
    }),
  );
  list(found?.tools).forEach((t, n) =>
    out.push({
      key: `tools:${n}`,
      section: "Tool",
      label: t.name,
      detail: [t.kind, t.source].filter(Boolean).join(" · "),
      on: true,
      entry: { name: t.name, kind: t.kind ?? "mcp", used_by: ["all"], writes: false },
    }),
  );
  list(found?.sandboxes).forEach((s, n) =>
    out.push({
      key: `sandboxes:${n}`,
      section: "Sandbox",
      label: s.name,
      detail: s.source ?? "",
      on: true,
      entry: { name: s.name, runs: "", can_reach: [], cannot_reach: [], used_by: ["all"] },
    }),
  );
  return out;
}

/** Adds the ticked candidates to the record, skipping those it already has. Returns how many were added. */
export function addCandidates(record, candidates) {
  let added = 0;
  for (const c of candidates) {
    if (!c.on) continue;
    const section = c.key.split(":")[0];
    const id = section === "instructions" ? "path" : "name";
    if (rowsAt(record, [section]).some((r) => r[id] === c.entry[id])) continue;
    addRow(record, [section], { ...c.entry });
    added++;
  }
  return added;
}

export const ALL = "all";
const PLAIN_AGENT = "plain";

/**
 * The choices of an agent picker: "all" when the field is a list, the catalog's
 * agents, plain (a session with no agent), then any name the record has that the
 * catalog lacks, marked so it is never silently dropped.
 * @param {string[]} catalog @param {string[]} chosen the names the record holds @param {boolean} withAll
 * @returns {{ value: string, label: string, checked: boolean }[]}
 */
export function agentChoices(catalog, chosen, withAll, notEnabled = []) {
  const known = [...(withAll ? [ALL] : []), ...catalog, ...(catalog.includes(PLAIN_AGENT) ? [] : [PLAIN_AGENT])];
  const label = (n) => (n === PLAIN_AGENT && !catalog.includes(PLAIN_AGENT) ? "plain (a session with no agent)" : n);
  const strays = chosen.filter((n, i) => typeof n === "string" && n !== "" && !known.includes(n) && chosen.indexOf(n) === i);
  return [
    ...known.map((n) => ({ value: n, label: label(n), checked: chosen.includes(n) })),
    ...strays.map((n) => ({ value: n, label: notEnabled.includes(n) ? `${n} (not enabled in this project)` : `${n} (not in the catalog)`, checked: true })),
  ];
}

/** The list after ticking or unticking a name: "all" excludes the agents, an agent excludes "all". */
export function toggled(list, name, on) {
  const rest = list.filter((n) => n !== name);
  if (!on) return rest;
  if (name === ALL) return [ALL];
  return [...rest.filter((n) => n !== ALL), name];
}

/** What a closed agent picker says. */
export const agentSummary = (list) => (list.length === 0 ? "No agents chosen" : list.join(", "));

/**
 * What the empty state says discovery found: rule files by name, the others
 * counted. "" when nothing was found.
 * @param {{ key: string, label: string, entry: { kind?: string } }[]} candidates
 */
export function discoveryText(candidates) {
  const of = (section) => candidates.filter((c) => c.key.startsWith(`${section}:`));
  const instructions = of("instructions");
  const rules = instructions.filter((c) => c.entry.kind === "rules").map((c) => c.label);
  const skills = instructions.filter((c) => c.entry.kind === "skill").length;
  const prompts = instructions.filter((c) => c.entry.kind === "subagent_prompt").length;
  const count = (n, one, many) => `${n} ${n === 1 ? one : many}`;
  const found = [
    ...rules,
    ...(skills ? [count(skills, "skill", "skills")] : []),
    ...(prompts ? [count(prompts, "sub-agent prompt", "sub-agent prompts")] : []),
    ...(of("tools").length ? [count(of("tools").length, "tool", "tools")] : []),
    ...(of("sandboxes").length ? [count(of("sandboxes").length, "sandbox", "sandboxes")] : []),
  ];
  if (found.length === 0) return "";
  const head = found.slice(0, -1).join(", ");
  const list = found.length === 1 ? found[0] : `${head} and ${found[found.length - 1]}`;
  return `We found ${list} in the repository. Confirm them to add them to the architecture.`;
}

/** A value as it is saved: without the empty strings, empty lists, nulls and false flags a draft grows while it is edited. */
function plain(v) {
  if (Array.isArray(v)) return v.map(plain);
  if (v === null || typeof v !== "object") return v;
  const out = {};
  for (const k of Object.keys(v).sort()) {
    const x = plain(v[k]);
    const empty = x === "" || x === null || x === undefined || x === false || (Array.isArray(x) && x.length === 0);
    if (!empty) out[k] = x;
  }
  return out;
}
const sameRow = (a, b) => JSON.stringify(plain(a)) === JSON.stringify(plain(b));

/**
 * Which rows of a draft list differ from the saved list. With as many rows as
 * were saved, row n is compared with saved row n; after a row was added or
 * removed, each row is compared with the saved row that has its key (a row
 * with no such saved row is new, so edited).
 * @param {any[] | undefined} saved @param {any[] | undefined} draft @param {(row: any) => string} keyOf
 * @returns {boolean[]}
 */
export function editedRows(saved, draft, keyOf) {
  const was = Array.isArray(saved) ? saved : [];
  const now = Array.isArray(draft) ? draft : [];
  if (was.length === now.length) return now.map((row, i) => !sameRow(row, was[i]));
  const taken = new Set();
  return now.map((row) => {
    const key = keyOf(row);
    const at = key === "" ? -1 : was.findIndex((r, i) => !taken.has(i) && keyOf(r) === key);
    if (at < 0) return true;
    taken.add(at);
    return !sameRow(row, was[at]);
  });
}

/**
 * The record's hand-offs as chains of agents, in order: a hand-off that starts
 * where the last one ended continues its chain, any other starts a new one.
 * A hand-off missing its from or its to is not drawn.
 * @param {{ from?: string, to?: string }[] | undefined} handoffs @returns {string[][]}
 */
export function handoffChains(handoffs) {
  const chains = [];
  for (const h of Array.isArray(handoffs) ? handoffs : []) {
    if (!h?.from || !h?.to) continue;
    const last = chains[chains.length - 1];
    if (last && last[last.length - 1] === h.from) last.push(h.to);
    else chains.push([h.from, h.to]);
  }
  return chains;
}

/** Whether an instruction path is one discovery did not list. Paths of harness:// are the harness's own, never in a checkout; null means discovery has not answered. @param {Set<string> | null} found */
export function isMissingPath(path, found) {
  const p = String(path ?? "").trim();
  if (!found || p === "" || p.startsWith("harness://")) return false;
  return !found.has(p);
}
