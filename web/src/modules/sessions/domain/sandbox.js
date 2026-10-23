// A session's sandbox violations: what the agent sandbox denied the processes
// of a session (the glossary's "Sandbox violation"), and how the Sandbox tab
// filters, groups and offers to allow them. Pure.

/**
 * @typedef {object} Violation
 * @property {Date} at
 * @property {string} operation  the platform's own word, e.g. file-read-data
 * @property {string} path       the file, or other target (an address); "" when the log named none
 * @property {string} process    the process's name
 *
 * @typedef {object} ViolationLog
 * @property {boolean} available  this platform collects violations at all
 * @property {readonly Violation[]} violations  newest first
 *
 * @typedef {"read" | "write" | "other"} Access
 *
 * @typedef {object} ViolationRow
 * @property {string} key
 * @property {Access} access
 * @property {string} operation
 * @property {string} path
 * @property {string} process
 * @property {number} count
 * @property {Date} first
 * @property {Date} last
 */

/** Where the Agent isolation settings are; the Sandbox tab links nowhere else. */
export const AGENT_ISOLATION_URL = "/agent-isolation";
/** The query parameter that opens the settings' extra paths with a path in the Add path field. */
export const ALLOW_PATH_PARAM = "allow_path";

/** The Agent isolation settings' sandbox section. */
export const sandboxSettingsHref = () => `${AGENT_ISOLATION_URL}#sandbox`;

/** The settings' extra paths, with path ready to add. It allows nothing by itself. */
export const allowPathHref = (path) => `${AGENT_ISOLATION_URL}?${ALLOW_PATH_PARAM}=${encodeURIComponent(path)}#sandbox`;

/** @returns {Access} */
export function accessOf(operation) {
  if (operation.startsWith("file-read")) return "read";
  if (operation.startsWith("file-write")) return "write";
  return "other";
}

/** Credential folders: the harness gives agents their own credentials, so the right answer is never to allow these. */
const CREDENTIALS = [
  { re: /\/\.ssh(\/|$)/, hint: "Agents never get your SSH keys. Git uses the harness git credential instead. Nothing to allow." },
  { re: /\/\.gnupg(\/|$)/, hint: "Agents never get your GPG keys. Nothing to allow." },
  { re: /\/\.aws(\/|$)/, hint: "Agents never get your cloud credentials. Nothing to allow." },
  { re: /\/\.config\/gh(\/|$)/, hint: "The harness gives agents their own GitHub credential, so gh does not need your login. Nothing to allow." },
  { re: /\/Library\/Keychains(\/|$)/, hint: "Agents never read your keychain. Nothing to allow." },
  { re: /\/\.claude(\.json)?(\/|$)/, hint: "The agent never reads your own Claude login. It uses the agent credential. Nothing to allow." },
];

/** Why the harness refuses this on purpose, when it is certain; "" otherwise. */
export function hintFor(violation) {
  return CREDENTIALS.find((c) => c.re.test(violation.path))?.hint ?? "";
}

/** A file path that may be offered for allowing: absolute, and no credential. */
export function allowable(violation) {
  return violation.path.startsWith("/") && hintFor(violation) === "";
}

/**
 * @param {readonly Violation[]} violations
 * @param {{ access?: Access | "all", query?: string }} filter  query matches the path or process, case-insensitively
 */
export function filterViolations(violations, { access = "all", query = "" } = {}) {
  const q = query.trim().toLowerCase();
  return violations.filter(
    (v) => (access === "all" || accessOf(v.operation) === access) && (q === "" || v.path.toLowerCase().includes(q) || v.process.toLowerCase().includes(q)),
  );
}

/**
 * The rows of a list of violations, newest first: with grouped, the same
 * operation, path and process are one row with a count.
 * @param {readonly Violation[]} violations  newest first
 * @returns {ViolationRow[]}
 */
export function violationRows(violations, grouped) {
  /** @type {Map<string, ViolationRow>} */
  const rows = new Map();
  violations.forEach((v, i) => {
    const key = grouped ? `${v.operation}\u0000${v.path}\u0000${v.process}` : `#${i}`;
    const row = rows.get(key);
    if (!row) {
      rows.set(key, { key, access: accessOf(v.operation), operation: v.operation, path: v.path, process: v.process, count: 1, first: v.at, last: v.at });
      return;
    }
    row.count += 1;
    if (v.at < row.first) row.first = v.at;
    if (v.at > row.last) row.last = v.at;
  });
  return [...rows.values()];
}

/** How many were denied, and how many different paths. */
export function summarize(violations) {
  return { denied: violations.length, paths: new Set(violations.map((v) => v.path)).size };
}
