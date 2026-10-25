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
