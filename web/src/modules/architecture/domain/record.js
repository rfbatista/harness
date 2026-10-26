// The repository architecture record as the page holds it (the contract
// "Repository architecture record and architecture file"): its tabs, the
// stat cards and tab counts, and the export states in words. Pure.

/**
 * A tab of the page. `slug` is the tab's own name and `path` the deep link
 * that opens it ("" is the page itself, which opens the first tab).
 * @typedef {{ slug: string, label: string, paths: string[] }} Tab
 */

/** @type {Tab[]} */
export const TABS = Object.freeze([
  { slug: "instructions", label: "Instructions & rules", paths: ["", "instructions"] },
  { slug: "tools", label: "Tools", paths: ["tools"] },
  { slug: "sandboxes", label: "Sandboxes", paths: ["sandboxes"] },
  { slug: "orchestration", label: "Orchestration", paths: ["orchestration"] },
  { slug: "hooks", label: "Hooks", paths: ["rules"] },
  { slug: "layers", label: "Layers", paths: ["layers"] },
  { slug: "output", label: "Output", paths: ["file", "diagram"] },
]);

/** The codes the architecture endpoints refuse with that the page acts on. */
export const ARCHITECTURE_CHANGED = "ARCHITECTURE_CHANGED";
export const ARCHITECTURE_INVALID = "ARCHITECTURE_INVALID";

/** The tab a deep link opens; the first tab for one that is unknown. */
export function tabForPath(path) {
  return TABS.find((t) => t.paths.includes(path)) ?? TABS[0];
}

/** The deep link that opens a tab, optionally in one of its views (Output: file or diagram). */
export function pathForTab(slug, view = "") {
  const tab = TABS.find((t) => t.slug === slug) ?? TABS[0];
  if (view && tab.paths.includes(view)) return view;
  return tab.paths[0];
}

const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * The six stat cards of the overview, in the order the page lists them.
 * `overlaps` is how many paths the layers preview says match more than one layer.
 * @param {any} record
 * @param {number} overlaps
 * @returns {{ key: string, value: number, label: string }[]}
 */
export function statCards(record, overlaps) {
  const r = record ?? {};
  const rules = list(r.rules);
  const blocking = rules.filter((x) => x.mode === "blocking").length;
  const routed = new Set(list(r.orchestration?.routing).map((x) => x.agent));
  return [
    { key: "instructions", value: list(r.instructions).length, label: "instruction files" },
    { key: "tools", value: list(r.tools).length, label: "tools" },
    { key: "sandboxes", value: list(r.sandboxes).length, label: "sandboxes" },
    { key: "agents", value: routed.size, label: "routed agents" },
    { key: "hooks", value: rules.length, label: `hook rules · ${blocking} blocking` },
    { key: "layers", value: list(r.layers).length, label: `layers · ${plural(overlaps, "overlap")}` },
  ];
}

/**
 * The count on each tab (Output has none). Orchestration counts routes and hand-offs.
 * @param {any} record
 * @returns {Record<string, number | null>}
 */
export function tabCounts(record) {
  const r = record ?? {};
  return {
    instructions: list(r.instructions).length,
    tools: list(r.tools).length,
    sandboxes: list(r.sandboxes).length,
    orchestration: list(r.orchestration?.routing).length + list(r.orchestration?.handoffs).length,
    hooks: list(r.rules).length,
    layers: list(r.layers).length,
    output: null,
  };
}

/**
 * The save bar's sum-up of a diff: "3 changes: 2 in Tools, 1 in Layers".
 * @param {{ pointer: string }[]} rows
 */
export function changeSummary(rows) {
  if (rows.length === 0) return "0 changes";
  const byTab = new Map();
  for (const row of rows) {
    const tab = TABS.find((t) => t.slug === tabOfPointer(row.pointer));
    byTab.set(tab.label, (byTab.get(tab.label) ?? 0) + 1);
  }
  const parts = [...byTab].map(([label, n]) => `${n} in ${label}`);
  return `${plural(rows.length, "change")}: ${parts.join(", ")}`;
}

/**
 * The export state as a badge: words first, a tone for those who see colour.
 * @param {{ state?: string } | null | undefined} exportInfo
 */
export function exportBadge(exportInfo) {
  switch (exportInfo?.state) {
    case "in_sync":
      return { label: `In sync with ${exportInfo.path || "the file"}`, tone: "success" };
    case "database_ahead":
      return { label: "Database ahead of the file", tone: "attention" };
    case "edited_outside":
      return { label: "File edited outside", tone: "attention" };
    case "file_invalid":
      return { label: "File invalid", tone: "danger" };
    case "never_exported":
      return { label: "Never exported", tone: "" };
    default:
      return null;
  }
}

/** A deep copy that no later edit of the original reaches. */
export const cloneRecord = (record) => JSON.parse(JSON.stringify(record));

/** Whether two records are the same, key order aside. */
export function sameRecord(a, b) {
  return canonical(a) === canonical(b);
}

/** The record as it is saved: a blank pattern is no pattern. */
export function cleanRecord(record) {
  const out = cloneRecord(record);
  if (typeof out.pattern === "string" && out.pattern.trim() === "") delete out.pattern;
  else if (typeof out.pattern === "string") out.pattern = out.pattern.trim();
  return out;
}

function canonical(v) {
  return JSON.stringify(v, (key, x) =>
    x && typeof x === "object" && !Array.isArray(x)
      ? Object.fromEntries(
          Object.entries(x)
            .filter(([k, y]) => !(k === "pattern" && y === ""))
            .sort(([p], [q]) => (p < q ? -1 : p > q ? 1 : 0)),
        )
      : x,
  );
}

const SECTIONS = {
  pattern: "Pattern",
  instructions: "Instructions & rules",
  tools: "Tools",
  sandboxes: "Sandboxes",
  orchestration: "Orchestration",
  layers: "Layers",
  unlayered: "Unlayered",
  rules: "Hooks",
};

/** The tab a JSON pointer into the record belongs to (an error's `path`). */
export function tabOfPointer(pointer) {
  const first = String(pointer).split("/")[1];
  switch (first) {
    case "instructions":
    case "pattern":
      return "instructions";
    case "rules":
      return "hooks";
    case "layers":
    case "unlayered":
      return "layers";
    case "tools":
    case "sandboxes":
    case "orchestration":
      return first;
    default:
      return "instructions";
  }
}

/** A JSON pointer as a person reads it: "/layers/0/name" is "Layers 1 › name". */
export function describePointer(pointer) {
  const parts = String(pointer).split("/").slice(1);
  if (parts.length === 0) return "Architecture";
  const [head, ...rest] = parts;
  const words = [SECTIONS[head] ?? head];
  for (const p of rest) words.push(/^\d+$/.test(p) ? String(Number(p) + 1) : p);
  return words.join(" › ");
}

/**
 * What changes from `before` to `after`, one entry per changed value, in the
 * record's own order. Lists are compared position by position.
 * @returns {{ pointer: string, label: string, before: string, after: string }[]}
 */
export function diffRecords(before, after) {
  const out = [];
  const show = (v) => (v === undefined ? "" : typeof v === "string" ? v : JSON.stringify(v));
  const walk = (a, b, pointer) => {
    const isObj = (v) => v !== null && typeof v === "object";
    if (isObj(a) && isObj(b) && Array.isArray(a) === Array.isArray(b)) {
      const keys = Array.isArray(a)
        ? Array.from({ length: Math.max(a.length, b.length) }, (_, i) => String(i))
        : [...new Set([...Object.keys(a), ...Object.keys(b)])];
      for (const k of keys) walk(a[k], b[k], `${pointer}/${k}`);
      return;
    }
    if (canonical(a) !== canonical(b)) {
      out.push({ pointer, label: describePointer(pointer), before: show(a), after: show(b) });
    }
  };
  walk(before ?? {}, after ?? {}, "");
  return out;
}
