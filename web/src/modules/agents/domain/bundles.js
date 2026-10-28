// What the Bundles tab shows and its small editor needs (UX spec section 3):
// rows with their filters, a bundle's detail, the rename's reach, the form's
// validity and the options of the skill-and-bundle picker. The server
// resolves bundles and does the renaming; this only arranges what it sent.

import { slugProblem } from "./names.js";

/** @typedef {import("./ports.js").Catalog} Catalog */

export const BUNDLE_FILTERS = [
  { id: "all", label: "All" },
  { id: "unused", label: "Unused" },
  { id: "nested", label: "Holds bundles" },
];

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const enc = encodeURIComponent;
const unusedBundle = (b) => b.usedByAgents.length === 0 && b.usedByBundles.length === 0;
const holdsBundles = (b) => b.entries.some((e) => e.startsWith("@"));

/** @param {Pick<Catalog, "bundles">} catalog @param {{ query?: string, filter?: string }} [o] */
export function bundleRows(catalog, { query = "", filter = "all" } = {}) {
  const q = query.trim().toLowerCase();
  const keep = { all: () => true, unused: unusedBundle, nested: holdsBundles }[filter] ?? (() => true);
  const matches = (b) => !q || [b.name, ...b.entries].some((s) => s.toLowerCase().includes(q));
  return catalog.bundles.filter((b) => keep(b) && matches(b)).map((b) => ({
    key: b.name,
    name: `@${b.name}`,
    href: `/agents/bundles/${enc(b.name)}`,
    description: b.entries.join(", "),
    meta: `${plural(b.resolvedSkills.length, "skill")} · ${unusedBundle(b) ? "unused" : plural(b.usedByAgents.length, "agent")}`,
  }));
}

/** @param {Pick<Catalog, "bundles">} catalog */
export function bundlesFooter(catalog) {
  return `agents.json › bundles · ${plural(catalog.bundles.length, "bundle")}`;
}

/**
 * The detail of the bundle `name`, or null.
 * @param {Catalog} catalog
 * @param {string} name
 */
export function bundleDetail(catalog, name) {
  const b = catalog.bundles.find((x) => x.name === name);
  if (!b) return null;
  const bundles = new Set(catalog.bundles.map((x) => x.name));
  const skills = new Set(catalog.skills.map((s) => s.name));
  return {
    key: b.name,
    title: `@${b.name}`,
    path: `agents.json › bundles.${b.name}`,
    copyPath: `${catalog.root}/agents.json`,
    sizeLabel: `${plural(b.resolvedSkills.length, "skill")} after nested bundles`,
    entriesHeading: `Contains · ${b.entries.length}`,
    entries: b.entries.map((e) => {
      const nested = e.startsWith("@");
      const n = nested ? e.slice(1) : e;
      return { label: e, kind: nested ? "bundle" : "skill", href: nested ? `/agents/bundles/${enc(n)}` : `/agents/skills/${enc(n)}`, missing: nested ? !bundles.has(n) : !skills.has(n) };
    }),
    unused: unusedBundle(b),
    agents: b.usedByAgents.map((n) => ({ name: n, href: `/agents/${enc(n)}` })),
    bundles: b.usedByBundles.map((n) => ({ name: `@${n}`, href: `/agents/bundles/${enc(n)}` })),
    editHref: `/agents/bundles/${enc(b.name)}/edit`,
    duplicateHref: `/agents/bundles/new?from=${enc(b.name)}`,
  };
}

/**
 * What a rename will rewrite: the agents and bundles that name `@previousName`.
 * @param {Pick<Catalog, "bundles">} catalog
 * @param {string} previousName  "" for a new bundle
 * @param {string} name  as typed
 */
export function renameImpact(catalog, previousName, name) {
  const b = previousName && name && previousName !== name ? catalog.bundles.find((x) => x.name === previousName) : undefined;
  if (!b) return { renamed: false, agents: [], bundles: [], text: "" };
  const names = [...b.usedByAgents, ...b.usedByBundles.map((n) => `@${n}`)];
  return {
    renamed: true,
    agents: b.usedByAgents,
    bundles: b.usedByBundles,
    text: `${plural(b.usedByAgents.length, "agent")} and ${plural(b.usedByBundles.length, "bundle")} (${names.join(", ") || "none"})`,
  };
}

/**
 * The form's starting point: the bundle as the catalog read it, or an empty one.
 * @param {Pick<Catalog, "bundles">} catalog
 * @param {string} name  "" for a new bundle
 * @returns {{ previousName: string, name: string, entries: string[] } | null}  null when there is no such bundle
 */
export function bundleDraft(catalog, name) {
  if (!name) return { previousName: "", name: "", entries: [] };
  const b = catalog.bundles.find((x) => x.name === name);
  return b ? { previousName: b.name, name: b.name, entries: [...b.entries] } : null;
}

/** The name as the field keeps it: the @ is the field's prefix, not part of the name. @param {string} typed */
export function cleanBundleName(typed) {
  return typed.replace(/^@+/, "");
}

/**
 * Whether the form is fit to send, and what is wrong with the name. An empty
 * name or no entries only turns Save off; the server says what else is wrong.
 * @param {{ name: string, entries: string[] }} draft
 */
export function checkBundleDraft(draft) {
  const nameProblem = draft.name === "" ? "" : slugProblem(draft.name, "Bundle name");
  return { valid: draft.name !== "" && nameProblem === "" && draft.entries.length > 0, nameProblem, entriesProblem: "" };
}

/**
 * How many things differ from what the editor read: the name, the entries.
 * @param {Pick<Catalog, "bundles">} catalog
 * @param {{ previousName: string, name: string, entries: string[] }} draft
 */
export function bundleChanges(catalog, draft) {
  const read = draft.previousName ? catalog.bundles.find((b) => b.name === draft.previousName) : undefined;
  if (!read) return (draft.name ? 1 : 0) + (draft.entries.length ? 1 : 0);
  const same = read.entries.length === draft.entries.length && read.entries.every((e, i) => e === draft.entries[i]);
  return (read.name === draft.name ? 0 : 1) + (same ? 0 : 1);
}

/** How many options the picker mounts at once. */
export const PICKER_PAGE = 50;

/**
 * The picker's options for a query: bundles first, then skills, each once.
 * `exclude` is the bundle being edited, which cannot hold itself.
 * @param {Pick<Catalog, "bundles" | "skills">} catalog
 * @param {{ query: string, chosen: string[], exclude: string, limit?: number }} o
 */
export function bundlePickerOptions(catalog, { query, chosen, exclude, limit = PICKER_PAGE }) {
  const q = query.trim().toLowerCase().replace(/^@/, "");
  const taken = new Set(chosen);
  const byName = (a, b) => a.name.localeCompare(b.name);
  const bundles = [...catalog.bundles].filter((b) => b.name !== exclude).sort(byName).map((b) => ({ key: `@${b.name}`, kind: "bundle", label: `@${b.name}`, hint: plural(b.resolvedSkills.length, "skill"), name: b.name }));
  const skills = [...catalog.skills].sort(byName).map((s) => ({ key: s.name, kind: "skill", label: s.name, hint: "", name: s.name }));
  const all = [...bundles, ...skills].filter((o) => !q || o.name.toLowerCase().includes(q)).map(({ name: _n, ...o }) => ({ ...o, chosen: taken.has(o.key) }));
  return { options: all.slice(0, limit), hidden: Math.max(0, all.length - limit), empty: all.length === 0 };
}

/**
 * Which form field a refusal belongs to: "name", "entries", or "form" (the
 * whole editor).
 * @param {import("./refusals.js").Refusal | null} refusal
 */
export function bundleFieldErrors(refusal) {
  const out = { name: "", entries: "", form: "" };
  if (!refusal) return out;
  const field = { BUNDLE_CYCLE: "entries", UNKNOWN_REFERENCE: "entries", NAME_TAKEN: "name", INVALID_INPUT: refusal.field }[refusal.code];
  out[field === "name" || field === "entries" ? field : "form"] = refusal.message;
  return out;
}
