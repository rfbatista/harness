// The design system page as it speaks: its tabs, the server's refusals, and
// the words of the state badge and the Save bar. Pure.

/**
 * A tab of the page; `slug` is its `?tab=` value. Proposals joins the list
 * when its slice lands.
 * @typedef {{ slug: string, label: string }} Tab
 */

/** @type {Tab[]} */
export const TABS = Object.freeze([
  { slug: "tokens", label: "Tokens" },
  { slug: "principles", label: "Principles" },
  { slug: "components", label: "Components" },
  { slug: "repositories", label: "Repositories" },
  { slug: "history", label: "History" },
]);

/** The codes the design system endpoints refuse with that the page acts on. */
export const CHANGED = "DESIGN_SYSTEM_CHANGED";
export const INVALID = "DESIGN_SYSTEM_INVALID";

/** The tab a `?tab=` value opens; the first for one that is unknown. */
export function tabForSlug(slug) {
  return TABS.find((t) => t.slug === slug) ?? TABS[0];
}

/** The errors of a DESIGN_SYSTEM_INVALID refusal: its details are { errors: [{ path, code, message }] }. */
export function invalidEntries(details) {
  const list = Array.isArray(details?.errors) ? details.errors : [];
  return list.filter((e) => typeof e?.path === "string").map((e) => ({ path: e.path, code: e.code ?? "", message: e.message ?? "" }));
}

const TOKEN_PATH = /^\/tokens\/(\d+)(?:\/([A-Za-z]+))?$/;

/**
 * Sorts a refusal's errors onto the rows they name. A token error's path is
 * /tokens/<position>[/<field>]; `rows` are in the order the page sent them.
 * Errors that name no row come back in `other`.
 * @param {{ path: string, code: string, message: string }[]} entries
 * @param {{ id: string }[]} rows
 */
export function sortErrors(entries, rows) {
  const byRow = {};
  const other = [];
  for (const e of entries) {
    const m = TOKEN_PATH.exec(e.path);
    const row = m ? rows[Number(m[1])] : undefined;
    if (!row) {
      other.push(e);
      continue;
    }
    (byRow[row.id] ??= []).push({ field: m[2] ?? "row", code: e.code, message: e.message });
  }
  return { byRow, other };
}

/** What the Save bar says the next save writes. */
export const saveBarText = (version) => `Saves as version ${version + 1}`;

/** What the draft changes, in words. */
export function changeText(counts, principlesChanged, componentsChanged = false) {
  const parts = [];
  if (counts.edited > 0) parts.push(`${counts.edited} edited`);
  if (counts.added > 0) parts.push(`${counts.added} added`);
  if (counts.removed > 0) parts.push(`${counts.removed} removed`);
  if (principlesChanged) parts.push("principles changed");
  if (componentsChanged) parts.push("components changed");
  return parts.length === 0 ? "No unsaved changes" : `Unsaved: ${parts.join(", ")}`;
}

/** The notice over edits when another save landed first. */
export const elsewhereText = (version) =>
  `${version > 0 ? `Version ${version} was` : "A newer version was"} saved elsewhere. Your edits are kept and were not saved.`;

/** The pill in the page head: what the page holds. */
export function stateBadge({ exists, started, version, dirty }) {
  if (!exists) return started || dirty ? { label: "Draft, not saved", tone: "attention" } : { label: "Not started", tone: "" };
  return dirty ? { label: `Version ${version} · unsaved changes`, tone: "attention" } : { label: `Version ${version}`, tone: "success" };
}
