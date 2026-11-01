// The instruction files of a repository, as "Contract: Harness server ↔ Web UI
// — instruction files" names them: a path, a kind (memory, skill, subagent), a
// version and content the harness owns. Pure. The record keeps the descriptive
// metadata (applies to, loaded, note) under its own kind words (rules, skill,
// subagent_prompt); this module merges the two.

export const FILE_KINDS = Object.freeze(["memory", "skill", "subagent"]);

const RECORD_KIND = { memory: "rules", skill: "skill", subagent: "subagent_prompt" };

/** The record's kind word for a file kind (an unknown kind stays as it is). */
export const recordKindOf = (kind) => RECORD_KIND[kind] ?? kind;

/** The path a new file starts with, by kind; `name` fills the skill or sub-agent name (a `<name>` placeholder while it is blank). */
export function prefillPath(kind, name = "") {
  const n = String(name).trim() || "<name>";
  if (kind === "memory") return "CLAUDE.md";
  if (kind === "skill") return `.claude/skills/${n}/SKILL.md`;
  if (kind === "subagent") return `.claude/agents/${n}.md`;
  return "";
}

const SKILL_FILE = /^(\.claude\/skills\/([^/]+))\/.+$/;

/**
 * Plain files and skill folders, in path order. A skill's files (SKILL.md
 * first) group under the folder `.claude/skills/<name>`.
 * @returns {({ type: "file", file: any } | { type: "skill", folder: string, name: string, files: any[] })[]}
 */
export function groupFiles(files) {
  const sorted = [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  const out = [];
  const folders = new Map();
  for (const file of sorted) {
    const m = file.kind === "skill" ? SKILL_FILE.exec(file.path) : null;
    if (!m) {
      out.push({ type: "file", file });
      continue;
    }
    let group = folders.get(m[1]);
    if (!group) {
      group = { type: "skill", folder: m[1], name: m[2], files: [] };
      folders.set(m[1], group);
      out.push(group);
    }
    group.files.push(file);
  }
  for (const group of folders.values()) {
    group.files.sort((a, b) => Number(b.path.endsWith("/SKILL.md")) - Number(a.path.endsWith("/SKILL.md")) || (a.path < b.path ? -1 : 1));
  }
  return out;
}

/**
 * The rows of the list: the record's entries in their order, then each file
 * with no entry, with the defaults (applies to all, loaded always). A row has
 * `file` (the file's metadata, or null when the harness holds none) and
 * `recorded` (whether the record describes it).
 */
export function mergeMetadata(files, entries) {
  const byPath = new Map(files.map((f) => [f.path, f]));
  const described = new Set();
  const rows = [];
  for (const entry of entries) {
    described.add(entry.path);
    rows.push({ ...entry, file: byPath.get(entry.path) ?? null, recorded: true });
  }
  for (const file of [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))) {
    if (described.has(file.path)) continue;
    rows.push({ path: file.path, kind: recordKindOf(file.kind), applies_to: ["all"], loaded: "always", note: "", file, recorded: false });
  }
  return rows;
}

/** Whether the text differs from what was loaded (nothing loaded counts as ""). */
export const isDirty = (loaded, text) => String(loaded ?? "") !== String(text ?? "");

/** Whether a save was refused because someone saved a newer version. */
export const isStale = (err) => err?.code === "INSTRUCTION_CHANGED";

/**
 * What a feed event means for the open file: "ignore" (not this file, or the
 * version we hold), "reload" (a clean file moved on), "notice" (an edited file
 * moved on: keep the text, say so), "removed" (deleted, or renamed away).
 * @param {{ path: string, version: number, dirty: boolean } | null} open
 * @param {{ path: string, version: number }} change
 */
export function decideOnFeed(open, change) {
  if (!open || change.path !== open.path) return "ignore";
  if (change.version === 0) return "removed";
  if (change.version === open.version) return "ignore";
  return open.dirty ? "notice" : "reload";
}

const REFUSALS = {
  CHECKOUT_DIRTY: "The repository's checkout has uncommitted changes. Commit or stash them, then try again.",
  WRONG_BRANCH: "The repository's checkout is not on its default branch. Switch it back to the default branch, then try again.",
  INSTRUCTION_NOT_IMPORTED: "That file is not in the harness yet. Import it first, then remove it from the repository.",
  INSTRUCTION_NOT_FOUND: "There is no such instruction file in the harness; it may have been deleted elsewhere.",
  INSTRUCTION_CHANGED: "Someone saved a newer version of this file; your text is kept, so reload to see theirs.",
  INSTRUCTION_EXISTS: "A file already exists at that path; pick another path.",
  INSTRUCTION_INVALID: "The path or the text is not valid for this kind of file; fix the marked fields.",
  REPOSITORY_NOT_FOUND: "This repository is not in the project any more.",
};

/** One sentence for a refusal code, or "" when the code has none. */
export const refusalText = (code) => REFUSALS[code] ?? "";

/** The errors of an INSTRUCTION_INVALID refusal's details, one per field: { path, code, message }. */
export function fieldErrorsOf(details) {
  const list = Array.isArray(details?.errors) ? details.errors : [];
  return list
    .filter((e) => typeof e?.path === "string")
    .map((e) => ({ path: e.path, code: e.code ?? "", message: e.message ?? "" }));
}

/** The message for a path that no file answers to. */
export const notFoundText = (path) => `There is no instruction file at ${path}.`;
