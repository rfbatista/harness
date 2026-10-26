// What changed on disk between two reads of the catalog, for the banner of the
// open item and the "changed on disk" badge of a list row (UX spec section 6).
// Only an item's own fields count: a skill is not "changed" because an agent
// started naming it. No value of an environment variable, header, command or
// URL is ever put in a line: a line says that it changed, not to what.

/**
 * @typedef {import("./ports.js").Catalog} Catalog
 * @typedef {"agent"|"bundle"|"skill"|"mcp"} Kind
 * @typedef {{ kind: Kind, name: string, gone: boolean, lines: string[] }} ItemChange
 */

/** Per kind: the catalog list, and the fields that are the item's own, with how each reads. */
const SHAPES = {
  agent: { list: "agents", file: "agents.json", fields: { description: "scalar", aliases: "names", entries: "names", mcpServers: "names", allowedTools: "names" } },
  bundle: { list: "bundles", file: "agents.json", fields: { entries: "names" } },
  skill: { list: "skills", file: "", fields: { description: "scalar", fileCount: "scalar" } },
  mcp: { list: "mcpServers", file: "mcp-servers.json", fields: { transport: "scalar", command: "scalar", args: "scalar", env: "scalar", url: "scalar", headers: "scalar" } },
};

const LABELS = {
  description: "description",
  aliases: "aliases",
  entries: "skills and bundles",
  mcpServers: "MCP servers",
  allowedTools: "allowed tools",
  fileCount: "files",
  transport: "transport",
  command: "command",
  args: "arguments",
  env: "environment variables",
  url: "URL",
  headers: "headers",
};

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const list = (names) => names.join(", ");

/** One field's line, or "" when it did not change. @param {string} field @param {"scalar"|"names"} how */
function line(field, how, before, after) {
  if (same(before, after)) return "";
  if (how === "names") {
    const added = after.filter((n) => !before.includes(n));
    const removed = before.filter((n) => !after.includes(n));
    const parts = [];
    if (added.length) parts.push(`added ${list(added)}`);
    if (removed.length) parts.push(`removed ${list(removed)}`);
    return `${LABELS[field]}: ${parts.join(", ") || "reordered"}`;
  }
  return `${LABELS[field]} changed`;
}

/**
 * The items whose own fields differ between two reads, by "kind:name". A file
 * this page wrote itself (its version is in `own.hashes`) and a skill it saved
 * (`own.skills`) are not changes from outside.
 * @param {Catalog | null} before
 * @param {Catalog | null} after
 * @param {{ hashes?: Set<string>, skills?: Set<string> }} [own]
 * @returns {Record<string, ItemChange>}
 */
export function catalogChanges(before, after, own = {}) {
  /** @type {Record<string, ItemChange>} */
  const out = {};
  if (!before || !after) return out;
  const hashes = own.hashes ?? new Set();
  const skills = own.skills ?? new Set();
  const fileIsOurs = (file) => {
    if (!file) return false;
    return before.versions[file] !== after.versions[file] && hashes.has(after.versions[file]);
  };
  for (const [kind, shape] of Object.entries(SHAPES)) {
    if (fileIsOurs(shape.file)) continue;
    const was = new Map(before[shape.list].map((i) => [i.name, i]));
    const now = new Map(after[shape.list].map((i) => [i.name, i]));
    for (const [name, old] of was) {
      if (kind === "skill" && skills.has(name)) continue;
      const item = now.get(name);
      if (!item) {
        out[`${kind}:${name}`] = { kind: /** @type {Kind} */ (kind), name, gone: true, lines: ["removed from the catalog"] };
        continue;
      }
      const lines = Object.entries(shape.fields).map(([f, how]) => line(f, /** @type {any} */ (how), old[f], item[f])).filter(Boolean);
      if (lines.length) out[`${kind}:${name}`] = { kind: /** @type {Kind} */ (kind), name, gone: false, lines };
    }
  }
  return out;
}

/** The key an open item answers to. @param {Kind} kind @param {string} name */
export const changeKey = (kind, name) => `${kind}:${name}`;

/** The banner's sentence for one change. @param {ItemChange} change */
export function changeSentence(change) {
  const word = { agent: "This agent", bundle: "This bundle", skill: "This skill", mcp: "This MCP server" }[change.kind];
  return change.gone ? `${word} was removed from the catalog on disk.` : `${word} changed on disk.`;
}

/** The tab an item is shown in, for the page's route. */
export const KIND_OF_TAB = { agents: "agent", skills: "skill", bundles: "bundle", mcp: "mcp" };

/**
 * Items of the open tab whose editor must hear of a change: the file a form
 * writes moved under it.
 * @param {Record<string, string>} based  the versions the editor read
 * @param {Record<string, string>} now  the versions the catalog has now
 */
export function movedFiles(based, now) {
  return Object.keys(based).filter((p) => p in now ? now[p] !== based[p] : false);
}
