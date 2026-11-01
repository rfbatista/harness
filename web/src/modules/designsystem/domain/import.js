// What an import from a repository shows: the repositories that hold design
// system files, and the preview of what parsed and what was skipped.

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * One entry of find_design_system_sources: a repository and the paths its
 * default branch holds ("" for a file it lacks).
 * @param {object} dto
 */
export function sourceRow(dto) {
  const designMd = dto.design_md ?? "";
  const tokens = dto.tokens ?? "";
  return {
    id: dto.repository_id ?? "",
    name: dto.name ?? "",
    designMd,
    tokens,
    /** What the select shows: the repository and the token file found there. */
    label: tokens ? `${dto.name} · ${tokens}` : `${dto.name} · ${designMd}`,
  };
}

/** The `##` headings of the principles, in order. */
export function principleSections(markdown) {
  return String(markdown ?? "")
    .split("\n")
    .map((line) => /^##\s+(.+?)\s*$/.exec(line)?.[1])
    .filter(Boolean);
}

/**
 * A preview_design_system_import answer as the preview panel reads it: the
 * tokens counted by group in the order the record has them, the principles'
 * sections, the components found and every skipped entry with its reason.
 * @param {object} dto
 */
export function previewView(dto) {
  const record = dto.record ?? {};
  const counts = dto.counts ?? {};
  const order = [];
  for (const t of list(record.tokens)) if (!order.includes(t.group)) order.push(t.group);
  const groups = order.filter((g) => counts[g] > 0).map((group) => ({ group, count: counts[group] }));
  const sections = principleSections(record.principles);
  const found = dto.found ?? {};
  return {
    groups,
    tokenCount: groups.reduce((n, g) => n + g.count, 0),
    sections,
    hasPrinciples: String(record.principles ?? "").trim() !== "",
    components: Number.isInteger(found.components) ? found.components : 0,
    skipped: list(dto.skipped).map((s) => ({ path: s.path ?? "", reason: s.reason ?? "" })),
  };
}
