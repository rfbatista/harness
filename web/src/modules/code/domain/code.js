// Code views: read-only code as the server contract "code browsing and diffs"
// shapes it (internal/domain/code.go), in camelCase. No I/O, no Alpine, no DOM.

/**
 * @typedef {"ref"|"workspace"} SourceKind
 *
 * @typedef {object} CodeSource  what code is read, as the server resolved it
 * @property {SourceKind} kind
 * @property {string} repositoryId
 * @property {string} workspaceId  "" for a ref
 * @property {string} ref          as asked (or the default); a workspace's branch
 * @property {string} commit       the full hash it resolved to
 * @property {boolean} gone        a workspace whose worktree is no longer on disk
 *
 * @typedef {"dir"|"file"|"symlink"|"submodule"} EntryKind
 * @typedef {"added"|"modified"|"deleted"|"renamed"|"copied"|"type_changed"|"untracked"} ChangeStatus
 *
 * @typedef {object} TreeEntry
 * @property {string} path
 * @property {string} name
 * @property {EntryKind} kind
 * @property {number|null} size     files only; null for a deleted file
 * @property {string} target        symlinks only, otherwise ""
 * @property {ChangeStatus|null} change  workspace only; a dir is "modified" when anything under it changed
 * @property {string[]} zones
 *
 * @typedef {{ source: CodeSource, path: string, entries: TreeEntry[] }} CodeTree
 * @typedef {{ source: CodeSource, paths: string[], truncated: boolean }} CodePaths
 *
 * @typedef {object} CodeFile
 * @property {CodeSource} source
 * @property {string} path
 * @property {number} size
 * @property {boolean} binary
 * @property {boolean} truncated    content is the first 1 MiB
 * @property {string} language
 * @property {string|null} content  null when binary
 * @property {number} lineCount
 * @property {string[]} zones
 *
 * @typedef {"context"|"add"|"del"} LineKind
 * @typedef {{ kind: LineKind, old: number|null, new: number|null, text: string, noNewline: boolean }} HunkLine
 * @typedef {{ oldStart: number, oldLines: number, newStart: number, newLines: number, header: string, lines: HunkLine[] }} Hunk
 *
 * @typedef {object} FileDiff
 * @property {string} path
 * @property {string} oldPath        renames and copies, otherwise ""
 * @property {ChangeStatus} status
 * @property {boolean} binary
 * @property {number} added          -1 for binary
 * @property {number} deleted        -1 for binary
 * @property {Hunk[]|null} hunks     null when withheld
 * @property {"too_large"|"binary"|null} withheld
 * @property {string} language
 * @property {string[]} zones
 *
 * @typedef {"all"|"branch"|"working"|"commit"|"range"} DiffScope
 *
 * @typedef {object} CodeDiff
 * @property {CodeSource} source     the head side
 * @property {string} base           full hash of the base side
 * @property {DiffScope} scope
 * @property {FileDiff[]} files
 * @property {number} added
 * @property {number} deleted
 * @property {boolean} truncated     hunks were withheld from some file by the totals limit
 *
 * @typedef {{ id: string, name: string, pattern: string, fileCount: number }} ZoneCount
 * @typedef {{ id: string, name: string, purpose: string, zones: ZoneCount[], fileCount: number }} ContextCount
 * @typedef {{ source: CodeSource, contexts: ContextCount[], unassignedZones: ZoneCount[], unzonedFileCount: number, fileCount: number }} CodeMap
 *
 * @typedef {{ workspaceId: string, head: string }} CodeChanged  the project feed's code_changed
 */

export const ENTRY_KINDS = Object.freeze(["dir", "file", "symlink", "submodule"]);
export const STATUSES = Object.freeze(["added", "modified", "deleted", "renamed", "copied", "type_changed", "untracked"]);
export const SCOPES = Object.freeze(["all", "branch", "working", "commit", "range"]);
export const WORKSPACE_SCOPES = Object.freeze(["all", "branch", "working"]);

/** Contract limits the UI words its states with. */
export const LIMITS = Object.freeze({
  /** A diff with path= past this many changed lines comes back withheld. */
  pathLines: 20_000,
  /** A full diff includes hunks for at most this many changed lines. */
  totalLines: 20_000,
  pathsShown: 50_000,
});

/** The letter and word of each change state, and of a directory holding changes. */
const MARKS = Object.freeze({
  added: ["A", "Added"],
  modified: ["M", "Modified"],
  deleted: ["D", "Deleted"],
  renamed: ["R", "Renamed"],
  copied: ["C", "Copied"],
  type_changed: ["T", "Type changed"],
  untracked: ["U", "Untracked"],
  contains: ["", "Contains changes"],
});

/**
 * The change-mark block's content for a state: its letter (none for a
 * directory, which shows a dot) and the word that names it.
 * @param {string} change  a ChangeStatus or "contains"
 * @returns {{ change: string, letter: string, word: string } | null}
 */
export function changeMark(change) {
  const mark = MARKS[change];
  return mark ? { change, letter: mark[0], word: mark[1] } : null;
}

/** A directory's mark: the server says "modified" when anything under it changed. */
export const dirMark = (change) => (change ? changeMark("contains") : null);

/** The file name of a path. */
export const baseName = (path) => path.slice(path.lastIndexOf("/") + 1);

/** The directory of a path, "" at the root (no trailing slash). */
export const dirName = (path) => {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
};

/** Each ancestor directory of a path, outermost first ("a/b/c.go" → ["a", "a/b"]). */
export function ancestors(path) {
  const parts = path.split("/").slice(0, -1);
  return parts.map((_, i) => parts.slice(0, i + 1).join("/"));
}

/** The short form of a commit hash. */
export const shortHash = (hash) => (hash ?? "").slice(0, 7);

/** "6.1 KB": a size the way the code views write it. */
export function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let n = bytes / 1024;
  let u = 0;
  while (n >= 1024 && u < units.length - 1) {
    n /= 1024;
    u += 1;
  }
  return `${n < 10 ? n.toFixed(1) : Math.round(n)} ${units[u]}`;
}

/** "24,310": counts the way the code views write them. */
export const formatCount = (n) => n.toLocaleString("en-US");

/** "1 file", "12 files". */
export const plural = (n, one, many = `${one}s`) => `${formatCount(n)} ${n === 1 ? one : many}`;

/** The language's name for a file head's meta ("go" → "Go"). */
export function languageName(id) {
  const names = {
    go: "Go", templ: "templ", javascript: "JavaScript", jsx: "JSX", typescript: "TypeScript", tsx: "TSX",
    json: "JSON", markdown: "Markdown", html: "HTML", css: "CSS", scss: "SCSS", sass: "Sass", less: "Less",
    yaml: "YAML", toml: "TOML", xml: "XML", sql: "SQL", shell: "Shell", python: "Python", ruby: "Ruby",
    rust: "Rust", java: "Java", kotlin: "Kotlin", swift: "Swift", c: "C", cpp: "C++", csharp: "C#",
    php: "PHP", lua: "Lua", protobuf: "Protobuf", graphql: "GraphQL", dockerfile: "Dockerfile",
    makefile: "Makefile", gomod: "go.mod", plaintext: "Plain text",
  };
  return names[id] ?? (id ? id[0].toUpperCase() + id.slice(1) : "Plain text");
}
