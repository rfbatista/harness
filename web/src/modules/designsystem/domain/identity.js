// What names a design system in a project, and how the project's list shows it.

const MAX_SLUG = 60;

/** The slug a name suggests: lowercase words joined by dashes, starting with a letter ("" when the name has none). */
export function suggestSlug(name) {
  const slug = String(name ?? "")
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^[^a-z]+/, "")
    .slice(0, MAX_SLUG);
  return slug.replace(/-+$/, "");
}

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * One entry of list_design_systems as the list shows it. Version 0 is an
 * identity with nothing saved yet; open proposals show only when there are some.
 * @param {object} dto
 */
export function listRow(dto) {
  const repositories = list(dto.repositories).map((r) => ({ id: r.repository_id ?? "", name: r.name ?? "", exportState: r.export_state ?? "" }));
  const version = Number.isInteger(dto.version) ? dto.version : 0;
  const proposals = Number.isInteger(dto.open_proposals) ? dto.open_proposals : 0;
  return {
    id: dto.design_system_id,
    name: dto.name ?? "",
    slug: dto.slug ?? "",
    description: dto.description ?? "",
    hasDescription: (dto.description ?? "") !== "",
    version,
    versionLabel: `v${version}`,
    repositories,
    usedBy: repositories.map((r) => r.name).join(", "),
    unused: repositories.length === 0,
    openProposals: proposals,
    hasProposals: proposals > 0,
    proposalsLabel: `${proposals} open`,
  };
}

/** The rows by name, ignoring case. */
export function sortRows(rows) {
  return rows.slice().sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

/**
 * A refusal of a name, slug or description as a fault of one field: SLUG_TAKEN
 * names it in details.field, an invalid input in the path of its first entry.
 * `field` is "" when the refusal names none; `next` says what to do.
 * @param {unknown} err
 * @returns {{ field: string, code: string, message: string }}
 */
export function faultOf(err) {
  const code = err?.code ?? "";
  const message = err instanceof Error ? err.message : "";
  const entry = Array.isArray(err?.details?.errors) ? err.details.errors[0] : null;
  const named = typeof err?.details?.field === "string" ? err.details.field : typeof entry?.path === "string" ? entry.path.replace(/^\//, "") : "";
  const field = ["name", "slug", "description"].includes(named) ? named : "";
  const fault = { field, code: entry?.code ?? code, message: entry?.message ?? message };
  return { ...fault, next: code === "SLUG_TAKEN" ? `Choose another ${field || "name"}.` : "Fix it and try again." };
}
