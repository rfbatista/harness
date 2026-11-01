// A repository's use of a design system, as the Repositories tab shows it
// (uses: it is applied to the design system the tab is about; appliedId and
// appliedName: whichever design system it uses, "" for none).

export const DEFAULT_PATH = ".harness/design-system/";

/** Stated beside every switch: what opting in does, and what it does not. */
export const EFFECT_NOTE =
  "Sessions in this repository get a pointer to the design system and can read it. Nothing is written to the repository until you export.";

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
    path,
    draft: path,
    export: dto?.export ?? null,
    saving: false,
    error: null,
  };
}

/** Where a repository stands with its export; until the export exists, a repository that uses the design system has none. */
export function exportLabel(row) {
  if (!row.uses) return "";
  return row.export ? "Exported" : "Not exported yet";
}
