// A repository's use of a design system, as the Repositories tab shows it
// (uses: it is applied to the design system the tab is about; appliedId and
// appliedName: whichever design system it uses, "" for none).

import { relativeTime } from "../../../shared/presentation/format.js";

export const DEFAULT_PATH = ".harness/design-system/";

/**
 * One repository of list_design_system_repositories, as a row the page edits:
 * the saved use and path, and a draft of the path.
 */
export function optInRow(dto) {
  const path = dto?.path || DEFAULT_PATH;
  return {
    id: dto?.repository_id ?? "",
    name: dto?.name ?? "",
    uses: dto?.applied_here === true,
    appliedId: dto?.design_system?.design_system_id ?? "",
    appliedName: dto?.design_system?.name ?? "",
    confirming: false,
    movingFrom: "",
    path,
    draft: path,
    export: dto?.export ?? null,
    /** An export in flight, its last answer ({ commit, version }), the refusal that stopped it, and the files an edited outside confirmation lists. */
    exporting: false,
    exported: null,
    exportError: null,
    review: null,
    saving: false,
    error: null,
  };
}

const STATES = {
  never_exported: { label: "Never exported", tone: "attention" },
  in_sync: { label: "In sync", tone: "signal" },
  behind: { label: "Behind", tone: "attention" },
  edited_outside: { label: "Edited outside the harness", tone: "attention" },
};

/** The state badge of a repository that uses the design system; null for one that does not, or whose export has not been read. */
export function exportBadge(row) {
  if (!row.uses || !row.export) return null;
  return STATES[row.export.state] ?? { label: row.export.state, tone: "" };
}

/** Where a repository stands with its export, in words: the state and, once exported, the version and commit. */
export function exportLabel(row, now = new Date()) {
  if (!row.uses) return "";
  const badge = exportBadge(row);
  if (!badge) return "Not exported yet";
  const e = row.export;
  if (e.exported_version == null) return "No files at " + row.path + " yet";
  const ago = e.exported_at ? relativeTime(new Date(e.exported_at), now) : "";
  const when = ago === "" ? "" : ago === "now" ? " just now" : ` ${ago} ago`;
  const files = e.state === "edited_outside" && Array.isArray(e.changed_files) && e.changed_files.length > 0 ? " · " + e.changed_files.join(", ") + " differ" : "";
  const commit = e.exported_commit ? " · commit " + e.exported_commit.slice(0, 7) : "";
  const version = e.state === "behind" ? `exported version ${e.exported_version}, current ${e.current_version}` : `version ${e.exported_version} exported${when}`;
  return version + commit + files;
}

/** The chip beside a repository: applied to this design system, using another, or using none. */
export function chip(row) {
  if (row.uses) return { label: "Applied here", tone: "signal" };
  if (row.appliedId !== "") return { label: "Uses " + row.appliedName, tone: "" };
  return { label: "No design system", tone: "" };
}
