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

/** Whether a skill folder holds no SKILL.md in the harness: the server skips such a folder when it delivers instructions, so agents never get it. @param {{ folder: string, files: { row: { file: any } }[] }} group */
export const lacksSkillMd = (group) => !group.files.some((e) => e.row.file && e.path === `${group.folder}/SKILL.md`);

/**
 * What the list draws, top to bottom: a row of the list as `{ folder: false, row,
 * index, inSkill, entry: "instruction" }` (`index` is the row's place in `rows`), and
 * before the files of a skill a folder header `{ folder: true, folderPath,
 * name, count, needsSkillMd, entry: null }`. The grouping is groupFiles'; the order is the
 * rows' own, a skill standing where its first row stands (SKILL.md first
 * inside it).
 */
export function listItems(rows) {
  const entries = rows.map((row, index) => ({ path: row.path, kind: row.file?.kind ?? row.kind, row, index }));
  const first = (g) => (g.type === "skill" ? Math.min(...g.files.map((f) => f.index)) : g.file.index);
  const items = [];
  const row = (e, inSkill) => items.push({ folder: false, row: e.row, index: e.index, inSkill, entry: "instruction" });
  for (const g of groupFiles(entries).sort((a, b) => first(a) - first(b))) {
    if (g.type === "file") {
      row(g.file, false);
      continue;
    }
    items.push({ folder: true, row: null, index: null, inSkill: false, folderPath: g.folder, name: g.name, count: g.files.length, needsSkillMd: lacksSkillMd(g), entry: null });
    g.files.forEach((e) => row(e, true));
  }
  return items;
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
  INSTRUCTION_DIFFERS: "The repository copy changed since you imported it. Import it again first.",
  INSTRUCTION_NOT_FOUND: "There is no such instruction file in the harness; it may have been deleted elsewhere.",
  INSTRUCTION_CHANGED: "Someone saved a newer version of this file; your text is kept, so reload to see theirs.",
  INSTRUCTION_EXISTS: "A file already exists at that path; pick another path.",
  INSTRUCTION_INVALID: "The path or the text is not valid for this kind of file; fix the marked fields.",
  REPOSITORY_NOT_FOUND: "This repository is not in the project any more.",
};

/** One sentence for a refusal code, or "" when the code has none. */
export const refusalText = (code) => REFUSALS[code] ?? "";

/**
 * The banner's rows: each copy the repository still holds, marked "differs" or
 * "same" against the harness's text, and `imported` once the harness holds that path.
 * @param {{ path: string, kind: string, differs: boolean }[]} copies
 * @param {{ path: string }[]} files the harness's files
 */
export function copyRows(copies, files) {
  const held = new Set(files.map((f) => f.path));
  return copies.map((c) => ({ path: c.path, kind: c.kind, differs: c.differs, mark: c.differs ? "differs" : "same", imported: held.has(c.path) }));
}

/** Whether Import does something for a copy: the harness lacks it, or holds a different text. */
export const canImportRow = (row) => !row.imported || row.differs;

/** The paths Import all takes. */
export const importablePaths = (rows) => rows.filter(canImportRow).map((r) => r.path);

/** The paths Remove from repository takes: the copies the harness already holds. */
export const removablePaths = (rows) => rows.filter((r) => r.imported).map((r) => r.path);

const filesText = (paths) => (paths.length === 1 ? paths[0] : `${paths.length} files`);

/** What the person is asked before the removal commits. */
export const removalAsk = (paths) =>
  `Remove ${filesText(paths)} from the repository? This makes one commit on the default branch that deletes ${paths.length === 1 ? "it" : "them"}, with no push. ${paths.length === 1 ? "It stays" : "They stay"} in the harness.`;

/** The warning while copies are in the repository and in the harness, or "" when none are. */
export const bothCopiesText = (count) =>
  count === 0 ? "" : `${count === 1 ? "1 file is" : `${count} files are`} in both the repository and the harness: both copies reach the agent until ${count === 1 ? "it is" : "they are"} removed from the repository.`;

/** One sentence for a refusal of an import or a removal: a path missing there means the file left the repository's default branch. */
export const copyRefusalText = (code) => (code === "INSTRUCTION_NOT_FOUND" ? "That file is no longer on the repository's default branch." : refusalText(code));

/** What the banner says when the checkout could not be read, with the server's message. */
export const unreadableText = (message) => `The repository could not be read, so its instruction files are not listed.${message ? ` ${message}` : ""}`;

/** The errors of an INSTRUCTION_INVALID refusal's details, one per field: { path, code, message }. */
export function fieldErrorsOf(details) {
  const list = Array.isArray(details?.errors) ? details.errors : [];
  return list
    .filter((e) => typeof e?.path === "string")
    .map((e) => ({ path: e.path, code: e.code ?? "", message: e.message ?? "" }));
}

/** The message for a path that no file answers to. */
export const notFoundText = (path) => `There is no instruction file at ${path}.`;

/**
 * Where an INSTRUCTION_INVALID refusal's errors show: `path` and `content` for
 * the two fields of the editor (the first error of each), `other` for any other
 * field (kind), so nothing the server said is dropped.
 * @param {{ path: string, message: string }[]} errors
 */
export function fieldMessages(errors) {
  const out = { path: "", content: "", other: "" };
  for (const e of errors) {
    const field = e.path === "/path" ? "path" : e.path === "/content" ? "content" : "other";
    if (out[field] === "") out[field] = e.message;
  }
  return out;
}

/** The path of a new file after its kind changed: a path still at the old kind's prefill (or blank) follows the new kind; a path the person typed stays. */
export function pathAfterKindChange(oldKind, path, newKind) {
  if (path === "" || path === prefillPath(oldKind)) return prefillPath(newKind);
  return path;
}

// Past this many cells the exact diff is not worth the wait; the whole text is replaced.
const MAX_DIFF_CELLS = 4_000_000;

const linesOf = (text) => {
  const lines = String(text ?? "").split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
};

/**
 * A line diff: lines only `before` has (del), only `after` has (ins), and the
 * shared ones (eq), in reading order, a removal before its insertion.
 * @returns {{ type: "eq" | "del" | "ins", text: string }[]}
 */
export function lineDiff(before, after) {
  const a = linesOf(before);
  const b = linesOf(after);
  const out = [];
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) out.push({ type: "eq", text: a[start++] });
  let endA = a.length;
  let endB = b.length;
  const tail = [];
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
    tail.unshift({ type: "eq", text: a[endA] });
  }
  const x = a.slice(start, endA);
  const y = b.slice(start, endB);
  if (x.length * y.length > MAX_DIFF_CELLS) {
    return [...out, ...x.map((text) => ({ type: "del", text })), ...y.map((text) => ({ type: "ins", text })), ...tail];
  }
  // lcs[i][j]: the longest common run of x[i..] and y[j..].
  const lcs = Array.from({ length: x.length + 1 }, () => new Uint32Array(y.length + 1));
  for (let i = x.length - 1; i >= 0; i--) {
    for (let j = y.length - 1; j >= 0; j--) {
      lcs[i][j] = x[i] === y[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  let i = 0;
  let j = 0;
  while (i < x.length || j < y.length) {
    if (i < x.length && j < y.length && x[i] === y[j]) {
      out.push({ type: "eq", text: x[i] });
      i++;
      j++;
    } else if (i < x.length && (j === y.length || lcs[i + 1][j] >= lcs[i][j + 1])) {
      out.push({ type: "del", text: x[i++] });
    } else {
      out.push({ type: "ins", text: y[j++] });
    }
  }
  return [...out, ...tail];
}
