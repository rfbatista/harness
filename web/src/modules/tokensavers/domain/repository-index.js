// A repository index as the Token savers page shows it: whose it is, its
// age, and whether a build runs or failed. The server paints the same words
// on first paint (tokensavers.IndexState in Go).

/**
 * @typedef {"building"|"built"|"failed"|"missing"} IndexState
 *
 * @typedef {object} RepositoryIndex
 * @property {string} repositoryId
 * @property {string} repositoryName
 * @property {string} projectName
 * @property {string} checkout
 * @property {Date | null} builtAt  null when there is no index
 * @property {boolean} building
 * @property {string} error         why the last build failed, or ""
 */

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "project / repository". @param {RepositoryIndex} idx */
export function indexName(idx) {
  return idx.projectName ? `${idx.projectName} / ${idx.repositoryName}` : idx.repositoryName;
}

/** @param {RepositoryIndex} idx @returns {IndexState} */
export function indexState(idx) {
  if (idx.building) return "building";
  if (idx.error) return "failed";
  return idx.builtAt ? "built" : "missing";
}

/** "Building…", "Built 3h ago", "Not built yet". @param {RepositoryIndex} idx @param {Date} now */
export function indexStateText(idx, now) {
  if (idx.building) return "Building…";
  if (!idx.builtAt) return "Not built yet";
  return `Built ${age(idx.builtAt, now)}`;
}

/** "just now", "5m ago", "3h ago", "2d ago". */
function age(then, now) {
  const ms = Math.max(0, now.getTime() - then.getTime());
  if (ms < MINUTE) return "just now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m ago`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)}h ago`;
  return `${Math.floor(ms / DAY)}d ago`;
}
