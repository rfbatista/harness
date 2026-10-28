// What the Skills tab shows, computed from the catalog read (UX spec section
// 3): rows with filters and badges, a page at a time because the catalog has
// hundreds of skills, the footer, and the file tree of an open skill.

import { problemBadge } from "./library.js";

/** @typedef {import("./ports.js").Catalog} Catalog */

export const SKILL_FILTERS = [
  { id: "all", label: "All" },
  { id: "unused", label: "Unused" },
  { id: "bundle", label: "In a bundle" },
  { id: "extra", label: "Has extra files" },
];

/** How many rows the list mounts at first, and each time "Show more" is chosen. */
export const SKILL_PAGE = 60;

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const enc = encodeURIComponent;

/**
 * @param {Catalog} catalog
 * @param {{ query?: string, filter?: string, limit?: number }} [o]
 * @returns {{ rows: object[], total: number, hidden: number }} total counts every match; hidden those past the limit
 */
export function skillRows(catalog, { query = "", filter = "all", limit = SKILL_PAGE } = {}) {
  const q = query.trim().toLowerCase();
  const keep = {
    all: () => true,
    unused: (s) => s.agentCount === 0,
    bundle: (s) => s.bundles.length > 0,
    extra: (s) => s.hasExtraFiles,
  }[filter] ?? (() => true);
  const matches = catalog.skills.filter((s) => keep(s) && (!q || s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)));
  const rows = matches.slice(0, limit).map((s) => ({
    name: s.name,
    href: `/agents/skills/${enc(s.name)}`,
    description: s.description || "No description",
    dim: s.description === "",
    meta: s.agentCount === 0 ? "unused" : `reaches ${plural(s.agentCount, "agent")}`,
    badge: problemBadge(s.problems),
  }));
  return { rows, total: matches.length, hidden: matches.length - rows.length };
}

/** @param {Pick<Catalog, "skills">} catalog */
export function skillsFooter(catalog) {
  return `skills/ · ${plural(catalog.skills.length, "folder")}`;
}

/**
 * What the catalog read says of the skill `name`, before its body arrives; null when there is none.
 * @param {Catalog} catalog
 * @param {string} name
 */
export function skillSummary(catalog, name) {
  const s = catalog.skills.find((x) => x.name === name);
  if (!s) return null;
  return {
    name: s.name,
    description: s.description,
    noDescription: s.description === "",
    badge: problemBadge(s.problems),
    unused: s.agentCount === 0,
    path: `skills/${s.name}/SKILL.md`,
    copyPath: `${catalog.root}/skills/${s.name}`,
    editHref: `/agents/skills/${enc(s.name)}/edit`,
    duplicateHref: `/agents/skills/new?from=${enc(s.name)}`,
  };
}

/**
 * Who a skill reaches, as links, from the skill read.
 * @param {{ agents: string[], bundles: string[], agentCount: number }} skill
 */
export function skillReach(skill) {
  return {
    agents: skill.agents.map((n) => ({ name: n, href: `/agents/${enc(n)}` })),
    bundles: skill.bundles.map((n) => ({ name: `@${n}`, href: `/agents/bundles/${enc(n)}` })),
    total: plural(skill.agentCount, "agent") + " in total",
  };
}

const sizeText = (n) => (n < 1024 ? `${n} B` : n < 1024 * 1024 ? `${Math.round(n / 1024)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`);

/**
 * The file tree of a skill, flattened in display order: SKILL.md first, then
 * each folder before its files, indented by depth.
 * @param {{ path: string, dir: boolean, size: number }[]} files  every file but SKILL.md
 */
export function skillFiles(files) {
  const root = { dirs: new Map(), files: [] };
  const folder = (parts) => parts.reduce((node, part) => {
    if (!node.dirs.has(part)) node.dirs.set(part, { dirs: new Map(), files: [] });
    return node.dirs.get(part);
  }, root);
  for (const f of files) {
    const parts = f.path.split("/").filter(Boolean);
    if (f.dir) folder(parts);
    else folder(parts.slice(0, -1)).files.push({ name: parts[parts.length - 1], size: f.size, path: f.path });
  }
  const rows = [{ key: "SKILL.md", name: "SKILL.md", dir: false, depth: 0, size: "" }];
  const walk = (node, depth, prefix) => {
    for (const [name, child] of [...node.dirs].sort(([a], [b]) => a.localeCompare(b))) {
      rows.push({ key: `${prefix}${name}/`, name, dir: true, depth, size: "" });
      walk(child, depth + 1, `${prefix}${name}/`);
    }
    for (const f of [...node.files].sort((a, b) => a.name.localeCompare(b.name))) rows.push({ key: f.path, name: f.name, dir: false, depth, size: sizeText(f.size) });
  };
  walk(root, 0, "");
  return rows;
}
