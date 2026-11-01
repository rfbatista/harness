// A repository's opt-in to the project design system, as the page and the
// repository settings both show it.

export const DEFAULT_PATH = ".harness/design-system/";

/** Stated beside every switch: what opting in does, and what it does not. */
export const EFFECT_NOTE =
  "Sessions in this repository get a pointer to the design system and can read it. Nothing is written to the repository until you export.";

/**
 * One repository of list_design_system_repositories, as a row the page edits:
 * the saved opt-in and path, and a draft of the path.
 */
export function optInRow(dto) {
  const path = dto?.path || DEFAULT_PATH;
  return {
    id: dto?.repository_id ?? "",
    name: dto?.name ?? "",
    uses: dto?.uses_design_system === true,
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
