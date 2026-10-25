// How the catalog's refusals (contract section 5) and problem codes
// (section 3) read to a person: the message, the field it belongs to and what
// to do next; the badge a row shows. The server's coded error body carries
// extra fields (field, suggestion, names, current_versions, changed); the api
// client keeps them on StructuredError.details.

import { Codes, codeOf } from "../../../shared/domain/errors.js";

const GONE = {
  AGENT_NOT_FOUND: "agent",
  SKILL_NOT_FOUND: "skill",
  BUNDLE_NOT_FOUND: "bundle",
  MCP_SERVER_NOT_FOUND: "MCP server",
  PROJECT_NOT_FOUND: "project",
};

/**
 * @typedef {object} Refusal
 * @property {string} code
 * @property {string} message
 * @property {string} field  the form field it belongs to; "" for the whole form
 * @property {string} next  what to do about it
 * @property {string} suggestion  NAME_TAKEN: a free name; else ""
 * @property {string[]} names  UNKNOWN_REFERENCE: what does not exist
 * @property {Record<string, string>} currentVersions  CATALOG_CHANGED
 * @property {{ path: string, theirs: string }[]} changed  CATALOG_CHANGED: each file's current text
 */

/**
 * @param {unknown} err
 * @param {{ undo?: boolean }} [context]  undo: the refusal answers an Undo
 * @returns {Refusal}
 */
export function describeRefusal(err, { undo = false } = {}) {
  const code = codeOf(err);
  const details = (err instanceof Error && /** @type {any} */ (err).details) || {};
  const out = {
    code,
    message: err instanceof Error && err.message ? err.message : "Something went wrong.",
    field: "",
    next: "",
    suggestion: "",
    names: [],
    currentVersions: {},
    changed: [],
  };
  if (code === Codes.INVALID_INPUT) {
    out.field = typeof details.field === "string" ? details.field : "";
    out.next = "Fix it and save again.";
  } else if (code === "NAME_TAKEN") {
    out.field = "name";
    out.suggestion = typeof details.suggestion === "string" ? details.suggestion : "";
    out.message = out.suggestion ? `That name is taken. Try ${out.suggestion}.` : "That name is taken.";
    out.next = "Pick a name nothing else uses.";
  } else if (code === "CATALOG_CHANGED") {
    out.currentVersions = details.current_versions ?? {};
    out.changed = Array.isArray(details.changed) ? details.changed : [];
    if (undo) {
      out.message = "A file changed since this save, so Undo would lose that change. Nothing was undone.";
      out.next = "Reload from disk to see what is there now.";
    } else {
      out.message = "A file changed on disk since you opened this. Reload to see it, or overwrite it with your version.";
      out.next = "Reload loses your edits; Overwrite replaces the file on disk.";
    }
  } else if (code === "CATALOG_BROKEN") {
    out.next = "Saving is off until the file is fixed by hand. Then check again.";
  } else if (code === "UNKNOWN_REFERENCE") {
    out.names = Array.isArray(details.names) ? details.names : [];
    out.message = out.names.length ? `These do not exist: ${out.names.join(", ")}.` : out.message;
    out.next = "Remove them or create them first.";
  } else if (code === "BUNDLE_CYCLE") {
    out.message = "Bundles cannot include each other, directly or through another bundle.";
    out.next = "Remove the bundle that closes the loop.";
  } else if (code === "CHANGE_NOT_UNDOABLE") {
    out.message = "That change can no longer be undone: only the latest change since the server started can.";
  } else if (GONE[code]) {
    out.message = `That ${GONE[code]} no longer exists.`;
    out.next = "Reload from disk to see what is there now.";
  }
  return out;
}

/**
 * @typedef {object} Badge
 * @property {string} code  the problem's code
 * @property {string} text
 * @property {"danger"|"attention"} tone
 * @property {number} count
 */

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

// Most serious first: a row shows the first badge.
const PROBLEMS = [
  ["BUNDLE_CYCLE", "danger", () => "Bundle cycle"],
  ["MISSING_SKILL", "danger", (n) => plural(n, "missing skill", "missing skills")],
  ["UNKNOWN_BUNDLE", "danger", (n) => plural(n, "unknown bundle", "unknown bundles")],
  ["UNKNOWN_MCP_SERVER", "danger", (n) => plural(n, "unknown MCP server", "unknown MCP servers")],
  ["INVALID_SKILL", "danger", () => "Invalid SKILL.md"],
  ["NO_DESCRIPTION", "attention", () => "No description"],
  ["SECRET_NOT_SET", "attention", (n) => (n === 1 ? "secret not set" : `${n} secrets not set`)],
];

/** The badge a live update puts on the open item. */
export const CHANGED_ON_DISK = Object.freeze({ code: "CHANGED_ON_DISK", text: "changed on disk", tone: "attention", count: 1 });

/**
 * One badge per problem code, most serious first.
 * @param {{ code: string, message?: string }[] | undefined | null} problems
 * @returns {Badge[]}
 */
export function problemBadges(problems) {
  const list = Array.isArray(problems) ? problems : [];
  const out = [];
  for (const [code, tone, text] of PROBLEMS) {
    const count = list.filter((p) => p.code === code).length;
    if (count > 0) out.push({ code, tone, text: text(count), count });
  }
  const known = new Set(PROBLEMS.map(([code]) => code));
  for (const p of list) {
    if (!known.has(p.code)) out.push({ code: p.code, tone: "attention", text: p.message || p.code, count: 1 });
  }
  return /** @type {Badge[]} */ (out);
}

/** The badge that replaces a row's meta line, or null. @param {Parameters<typeof problemBadges>[0]} problems */
export function rowBadge(problems) {
  return problemBadges(problems)[0] ?? null;
}
