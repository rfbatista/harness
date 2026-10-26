// What the export state and an export refusal mean, in words a person acts
// on. Pure.

const PATH = ".harness/architecture.yaml";

/**
 * The export state: a title, what it means and what to do next.
 * @param {{ state?: string, path?: string } | null | undefined} info
 */
export function describeExport(info) {
  const path = info?.path || PATH;
  switch (info?.state) {
    case "in_sync":
      return { title: "In sync", text: `The file ${path} in the repository holds exactly this saved version.`, next: "" };
    case "database_ahead":
      return {
        title: "Database ahead of the file",
        text: `The saved architecture has changes the file ${path} does not have yet.`,
        next: "Export to commit the saved version to the repository.",
      };
    case "edited_outside":
      return {
        title: "File edited outside",
        text: `The file ${path} was changed outside the harness. Changes made there are not read back yet.`,
        next: "Export to replace the file with the saved version, or keep the file and ignore this.",
      };
    case "file_invalid":
      return {
        title: "File invalid",
        text: `The file ${path} is not valid YAML for the architecture schema, so it cannot be compared.`,
        next: "Export to replace it with the saved version.",
      };
    case "never_exported":
      return {
        title: "Never exported",
        text: `The architecture lives only in the database. There is no ${path} in the repository yet.`,
        next: "Export to commit the file to the repository's default branch.",
      };
    default:
      return null;
  }
}

/**
 * An export refusal: its code, what it means and what to do next.
 * @param {{ code?: string, message?: string } | null | undefined} err
 * @returns {{ code: string, title: string, text: string, next: string } | null} null for an error that is not a known refusal
 */
export function describeRefusal(err) {
  switch (err?.code) {
    case "CHECKOUT_DIRTY":
      return {
        code: "CHECKOUT_DIRTY",
        title: "The checkout has uncommitted changes",
        text: "The export commits on the repository's default branch, and it will not do that on top of uncommitted work.",
        next: "Commit or stash the changes in the repository's checkout, then export again.",
      };
    case "WRONG_BRANCH":
      return {
        code: "WRONG_BRANCH",
        title: "The checkout is not on its default branch",
        text: "The export commits on the repository's default branch, and the checkout is on another branch.",
        next: "Switch the checkout to its default branch, then export again.",
      };
    case "ARCHITECTURE_CHANGED":
      return {
        code: "ARCHITECTURE_CHANGED",
        title: "The architecture changed since this page loaded",
        text: "A newer version was saved, so what this page shows is not what would be exported.",
        next: "Reload the page to see the newest version, then export again.",
      };
    case "ARCHITECTURE_NOT_FOUND":
      return {
        code: "ARCHITECTURE_NOT_FOUND",
        title: "There is no saved architecture",
        text: "Only a saved architecture can be exported.",
        next: "Save the architecture first.",
      };
    default:
      return null;
  }
}
