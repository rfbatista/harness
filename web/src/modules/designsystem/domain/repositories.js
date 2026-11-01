// A repository's use of a design system, as the Repositories tab shows it
// (uses: it is applied to the design system the tab is about; appliedId and
// appliedName: whichever design system it uses, "" for none).

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
    saving: false,
    error: null,
  };
}

/** Where a repository stands with its export; until the export exists, a repository that uses the design system has none. */
export function exportLabel(row) {
  if (!row.uses) return "";
  return row.export ? "Exported" : "Not exported yet";
}

/** The chip beside a repository: applied to this design system, using another, or using none. */
export function chip(row) {
  if (row.uses) return { label: "Applied here", tone: "signal" };
  if (row.appliedId !== "") return { label: "Uses " + row.appliedName, tone: "" };
  return { label: "No design system", tone: "" };
}
