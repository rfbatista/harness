// What the export state and an export refusal mean, in words a person acts
// on. Pure.

const PATH = ".harness/architecture.yaml";

/**
 * The export state: a title, its tone, a headline and a one-line summary for
 * the banner and the sync node, what it means and what to do next.
 * @param {{ state?: string, path?: string } | null | undefined} info
 */
export function describeExport(info) {
  const path = info?.path || PATH;
  switch (info?.state) {
    case "in_sync":
      return {
        title: "In sync",
        tone: "success",
        summary: "Identical",
        text: `The file ${path} in the repository holds exactly this saved version.`,
        next: "",
      };
    case "database_ahead":
      return {
        title: "Database ahead of the file",
        tone: "signal",
        summary: "Changes not in the file",
        text: `The saved architecture has changes the file ${path} does not have yet.`,
        next: "Export to commit the saved version to the repository.",
      };
    case "edited_outside":
      return {
        title: "File edited outside",
        tone: "attention",
        summary: "File changed after the last export",
        text: `The file ${path} was changed outside the harness. Changes made there are not read back yet.`,
        next: "Export to replace the file with the saved version, or keep the file and ignore this.",
      };
    case "file_invalid":
      return {
        title: "File invalid",
        tone: "danger",
        summary: "Cannot be compared",
        text: `The file ${path} is not valid YAML for the architecture schema, so it cannot be compared.`,
        next: "Export to replace it with the saved version.",
      };
    case "never_exported":
      return {
        title: "Never exported",
        tone: "signal",
        summary: "No export yet",
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

const shortSha = (sha) => String(sha ?? "").slice(0, 7);

/**
 * What the three nodes above the file say: the database, the arrows between
 * the nodes, and the repository.
 * @param {{ state?: string, path?: string, exported_version?: number, exported_commit?: string } | null | undefined} info
 * @param {{ exists: boolean, version: number }} saved
 * @returns {{ database: string, repository: string, arrows: [string, string] }}
 */
export function exportNodes(info, saved) {
  const path = info?.path || PATH;
  const database = saved.exists ? `Saved version ${saved.version}` : "Nothing saved yet";
  let repository = `No file at ${path} yet`;
  if (info?.exported_commit) {
    const version = info.state === "in_sync" ? "" : ` (version ${info.exported_version})`;
    repository = `Default branch · commit ${shortSha(info.exported_commit)}${version}`;
  }
  const arrows = info?.state === "in_sync" ? ["=", "="] : ["→", "←"];
  return { database, repository, arrows };
}

/**
 * The schema checks: what was validated and how the file in the repository
 * stands. A saved version was validated by the server when it was saved.
 * @returns {{ badge: string, tone: string, items: string[] }}
 */
export function schemaChecks(info, saved) {
  if (!saved.exists) {
    return { badge: "Nothing saved", tone: "", items: ["Save the architecture first: it is checked against the schema when it is saved."] };
  }
  const items = [
    `Saved version ${saved.version} was checked against the architecture schema when it was saved.`,
    "Every layer pattern and rule regex compiles.",
    "The export also writes .harness/architecture.schema.json beside the file.",
  ];
  if (info?.state === "file_invalid") {
    const path = info.path || PATH;
    items.push(`The file ${path} in the repository is not valid for the schema, so it is not compared.`);
    return { badge: "File invalid", tone: "danger", items };
  }
  return { badge: "Valid", tone: "success", items };
}

/**
 * The YAML as numbered lines: each keeps its newline, so the pieces join back
 * into the file. A line is a comment, or a leading indent and dash, a key and
 * the rest.
 * @returns {{ n: number, text: string, comment: boolean, lead: string, key: string, rest: string }[]}
 */
export function yamlLines(yaml) {
  if (!yaml) return [];
  return yaml.split(/(?<=\n)/).map((text, i) => {
    if (/^\s*#/.test(text)) return { n: i + 1, text, comment: true, lead: "", key: "", rest: text };
    const m = text.match(/^(\s*(?:- )?)([A-Za-z_][\w-]*)(:[\s\S]*)$/);
    if (!m) return { n: i + 1, text, comment: false, lead: "", key: "", rest: text };
    return { n: i + 1, text, comment: false, lead: m[1], key: m[2], rest: m[3] };
  });
}
