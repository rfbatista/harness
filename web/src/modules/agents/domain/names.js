// The contract's name rules (contract section 1): agent, skill and bundle
// names are slugs; MCP server names are looser. Also the ${VAR} references
// MCP env and header values hold. Pure: the server enforces the same rules
// and has the last word.

const SLUG = /^[a-z0-9][a-z0-9-]{0,63}$/;
const MCP_NAME = /^[A-Za-z0-9_-]{1,64}$/;
const VAR_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** @param {unknown} name */
export function isSlug(name) {
  return typeof name === "string" && SLUG.test(name);
}

/** @param {unknown} name */
export function isMcpName(name) {
  return typeof name === "string" && MCP_NAME.test(name);
}

/**
 * What is wrong with a slug, or "".
 * @param {string} name
 * @param {string} label  how the field reads: "Skill name"
 */
export function slugProblem(name, label) {
  if (!name) return `${label} is required.`;
  if (name.length > 64) return `${label} is at most 64 characters.`;
  if (!SLUG.test(name)) return `${label} uses lowercase letters, digits and hyphens, and starts with a letter or digit.`;
  return "";
}

/** What is wrong with an MCP server name, or "". @param {string} name */
export function mcpNameProblem(name) {
  if (!name) return "Server name is required.";
  if (name.length > 64) return "Server name is at most 64 characters.";
  if (!MCP_NAME.test(name)) return "Server name uses letters, digits, underscores and hyphens.";
  return "";
}

/**
 * The variables a value references, each once, in order.
 * @param {unknown} text
 * @returns {string[]}
 */
export function placeholderNames(text) {
  if (typeof text !== "string") return [];
  const names = [];
  for (const m of text.matchAll(/\$\{([^}]*)\}/g)) {
    if (VAR_NAME.test(m[1]) && !names.includes(m[1])) names.push(m[1]);
  }
  return names;
}

/** Whether a value is exactly one ${VAR}. @param {unknown} text */
export function isPlaceholder(text) {
  return typeof text === "string" && /^\$\{[A-Za-z_][A-Za-z0-9_]*\}$/.test(text);
}

/** What is wrong with the first malformed ${…} in a value, or "". @param {unknown} text */
export function placeholderProblem(text) {
  if (typeof text !== "string") return "";
  for (const m of text.matchAll(/\$\{([^}]*)\}/g)) {
    if (m[1] === "") return "${} names no variable.";
    if (!VAR_NAME.test(m[1])) return `${m[0]} is not a variable name: use letters, digits and underscores, not starting with a digit.`;
  }
  const open = text.match(/\$\{[^}]*$/);
  if (open) return `${open[0]} is missing its closing brace.`;
  return "";
}

/**
 * A catalog folder as the header names it: the last two folders behind an
 * ellipsis ("…/data/agents/"), so a long absolute path never widens the
 * page. A path of two folders or fewer, or a placeholder like
 * "<data dir>/agents/", stays whole; the full path goes in the title.
 * @param {unknown} path
 */
export function shortRoot(path) {
  if (typeof path !== "string" || path === "") return "";
  const parts = path.split("/").filter((p) => p !== "");
  if (parts.length <= 2 || path.includes("<")) return path;
  return `…/${parts.slice(-2).join("/")}/`;
}
