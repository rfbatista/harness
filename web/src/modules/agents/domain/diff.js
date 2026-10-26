// The diff the page draws from the server's answers (contract section 4): the
// `writes` of a dry run (before and after of every file the save touches) and
// the `theirs` text of a CATALOG_CHANGED refusal. The page never computes file
// bytes: it only lays the two texts the server gave side by side, line by line.

/**
 * @typedef {object} DiffLine
 * @property {"add"|"del"|"same"|"gap"} kind
 * @property {"+"|"−"|" "|""} mark
 * @property {string} text
 * @property {number} key  its place in the file, for the page's lists
 *
 * @typedef {object} DiffFile
 * @property {string} path
 * @property {"new"|"deleted"|"changed"|"same"|"binary"|"current"} status
 * @property {string} label  "new file", "deleted", "+2 −1" …
 * @property {number} added
 * @property {number} removed
 * @property {DiffLine[]} lines
 */

const CONTEXT = 3;
/** Beyond this many line pairs the middle is shown as replaced, not matched. */
const MAX_CELLS = 4_000_000;

const linesOf = (text) => {
  if (text === "") return [];
  const out = text.split("\n");
  if (out[out.length - 1] === "") out.pop();
  return out;
};

const line = (kind, text) => ({ kind, mark: { add: "+", del: "−", same: " ", gap: "" }[kind], text });

/** The edit script turning `a` into `b`: lines tagged same, del or add. */
function edits(a, b) {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }
  const head = a.slice(0, start).map((t) => line("same", t));
  const tail = a.slice(endA).map((t) => line("same", t));
  const x = a.slice(start, endA);
  const y = b.slice(start, endB);
  if (x.length * y.length > MAX_CELLS) return [...head, ...x.map((t) => line("del", t)), ...y.map((t) => line("add", t)), ...tail];
  // Longest common subsequence of the differing middle.
  const w = y.length + 1;
  const table = new Uint32Array((x.length + 1) * w);
  for (let i = x.length - 1; i >= 0; i--) {
    for (let j = y.length - 1; j >= 0; j--) {
      table[i * w + j] = x[i] === y[j] ? table[(i + 1) * w + j + 1] + 1 : Math.max(table[(i + 1) * w + j], table[i * w + j + 1]);
    }
  }
  const middle = [];
  let i = 0;
  let j = 0;
  while (i < x.length || j < y.length) {
    if (i < x.length && j < y.length && x[i] === y[j]) {
      middle.push(line("same", x[i]));
      i++;
      j++;
    } else if (i < x.length && (j === y.length || table[(i + 1) * w + j] >= table[i * w + j + 1])) middle.push(line("del", x[i++]));
    else middle.push(line("add", y[j++]));
  }
  return [...head, ...middle, ...tail];
}

/** Keeps the changed lines and CONTEXT lines around them; the rest becomes one gap line. */
function withContext(all) {
  const keep = new Array(all.length).fill(false);
  all.forEach((l, i) => {
    if (l.kind === "same") return;
    for (let k = Math.max(0, i - CONTEXT); k <= Math.min(all.length - 1, i + CONTEXT); k++) keep[k] = true;
  });
  const out = [];
  all.forEach((l, i) => {
    if (keep[i]) out.push(l);
    else if (out.length && out[out.length - 1].kind !== "gap") out.push(line("gap", "…"));
  });
  return out;
}

const count = (lines, kind) => lines.filter((l) => l.kind === kind).length;

/** @returns {DiffFile} */
function file(path, status, label, lines) {
  return { path, status, label, added: count(lines, "add"), removed: count(lines, "del"), lines: lines.map((l, key) => ({ ...l, key })) };
}

/**
 * One entry per file a save writes.
 * @param {{ path: string, before: string | null, after: string | null, encoding?: string }[]} writes
 * @returns {DiffFile[]}
 */
export function diffModel(writes) {
  return writes.map((w) => {
    if (w.encoding === "base64") return file(w.path, "binary", "binary file", []);
    if (w.before === null && w.after === null) return file(w.path, "same", "no change", []);
    if (w.before === null) return file(w.path, "new", "new file", linesOf(w.after ?? "").map((t) => line("add", t)));
    if (w.after === null) return file(w.path, "deleted", "deleted", linesOf(w.before).map((t) => line("del", t)));
    if (w.before === w.after) return file(w.path, "same", "no change", []);
    const lines = withContext(edits(linesOf(w.before), linesOf(w.after)));
    const f = file(w.path, "changed", "", lines);
    f.label = `+${f.added} −${f.removed}`;
    return f;
  });
}

/**
 * What changed on disk since the editor read it, from the refusal's `theirs`
 * and the text the editor last saw (`seen`, by path). With no earlier text,
 * the file is shown as it is now.
 * @param {{ path: string, theirs: string | null }[]} changed
 * @param {Record<string, string | null>} seen
 * @returns {DiffFile[]}
 */
export function conflictModel(changed, seen) {
  return changed.map((c) => {
    if (c.theirs === null) return file(c.path, "deleted", "deleted on disk", []);
    const before = seen[c.path];
    if (typeof before !== "string") return file(c.path, "current", "as it is now", linesOf(c.theirs).map((t) => line("same", t)));
    return diffModel([{ path: c.path, before, after: c.theirs }])[0];
  });
}
