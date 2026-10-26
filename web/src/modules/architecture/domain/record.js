// The repository architecture record as the page holds it (the contract
// "Repository architecture record and architecture file"): its tabs, the
// one-line count per part, and the export states in words. Pure.

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

/**
 * The one-line count of each part, in the order the page lists them.
 * @param {any} record
 * @returns {{ key: string, label: string, text: string }[]}
 */
export function summarize(record) {
  const r = record ?? {};
  const list = (xs) => (Array.isArray(xs) ? xs : []);
  const rules = list(r.rules);
  const blocking = rules.filter((x) => x.mode === "blocking").length;
  const routes = list(r.orchestration?.routing).length;
  const handoffs = list(r.orchestration?.handoffs).length;
  return [
    { key: "instructions", label: "Instructions & rules", text: plural(list(r.instructions).length, "file") },
    { key: "tools", label: "Tools", text: plural(list(r.tools).length, "tool") },
    { key: "sandboxes", label: "Sandboxes", text: plural(list(r.sandboxes).length, "sandbox", "sandboxes") },
    { key: "orchestration", label: "Orchestration", text: `${plural(routes, "route")} · ${plural(handoffs, "hand-off")}` },
    { key: "hooks", label: "Hooks", text: `${plural(rules.length, "rule")} · ${blocking} blocking` },
    { key: "layers", label: "Layers", text: plural(list(r.layers).length, "layer") },
  ];
}

/**
 * The export state as a badge: words first, a tone for those who see colour.
 * @param {{ state?: string } | null | undefined} exportInfo
 */
export function exportBadge(exportInfo) {
  switch (exportInfo?.state) {
    case "in_sync":
      return { label: "In sync with the file", tone: "" };
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
