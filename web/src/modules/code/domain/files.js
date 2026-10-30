// The Files view's tree: a session's virtual file system as roots (worktree,
// resources) whose folders load a page at a time. State is plain data, so a
// component can keep it reactive:
//
//   roots:    FilesRoot[]                       the roots route's answer
//   levels:   { [key]: Level }                  loaded folders, by levelKey
//   expanded: { [key]: true }                   open roots and folders
//
// A folder's key is "<root>:<dir>", the dir "" for the root itself.

/**
 * @typedef {{ root: string, label: string, available: boolean, note: string }} FilesRoot
 * @typedef {{ name: string, kind: "dir"|"file"|"symlink", ignored: boolean, size: number, target: string }} FilesEntry
 * @typedef {{ entries: FilesEntry[], next: string, loading: boolean, error: string }} Level
 * @typedef {{ roots: FilesRoot[], levels: Record<string, Level>, expanded: Record<string, boolean> }} FilesTree
 */

export const AGENT_STATES = Object.freeze(["working", "waiting", "idle", "ended"]);

/** @returns {FilesTree} */
export const emptyFilesTree = () => ({ roots: [], levels: {}, expanded: {} });

export const levelKey = (root, dir) => `${root}:${dir}`;
export const joinPath = (dir, name) => (dir ? `${dir}/${name}` : name);

/** @returns {Level} */
export const newLevel = () => ({ entries: [], next: "", loading: false, error: "" });

/** What the strip says of the agent, for each state: the status block's state, a word and a line. */
export function agentWords(state) {
  switch (state) {
    case "waiting":
      return { status: "waiting", word: "Agent is waiting on you", text: "An approval or a question is open on the Agent tab." };
    case "idle":
      return { status: "idle", word: "Agent is idle", text: "It waits for input: a quiet moment to step in." };
    case "ended":
      return { status: "done", word: "Session ended", text: "Nothing is running. The files stay as the agent left them." };
    default:
      return { status: "running", word: "Agent is working", text: "Mid-turn. Wait for it to stop before you edit the same files." };
  }
}

/**
 * The rows on screen, in order: each root and, under each open one, its
 * loaded entries, then a skeleton while its first page loads, a paging row
 * while more remain, or the error that stopped it. An unavailable root shows
 * its note instead.
 * @param {FilesTree} tree
 */
export function filesRows(tree) {
  const rows = [];
  for (const root of tree.roots) {
    const key = levelKey(root.root, "");
    const open = root.available && !!tree.expanded[key];
    rows.push({ type: "root", key: `root:${root.root}`, root, level: key, open, depth: 0 });
    if (!root.available) {
      rows.push({ type: "note", key: `note:${root.root}`, text: root.note, depth: 0 });
      continue;
    }
    if (open) walk(tree, root.root, "", 1, rows);
  }
  return rows;
}

function walk(tree, root, dir, depth, rows) {
  const key = levelKey(root, dir);
  const level = tree.levels[key];
  if (!level || (level.loading && level.entries.length === 0)) {
    for (let i = 0; i < 3; i += 1) rows.push({ type: "skeleton", key: `skeleton:${key}:${i}`, depth });
    return;
  }
  level.entries.forEach((entry, i) => {
    const path = joinPath(dir, entry.name);
    const open = entry.kind === "dir" && !!tree.expanded[levelKey(root, path)];
    rows.push({ type: "entry", key: `entry:${root}:${path}`, root, dir, path, entry, depth, open, pos: i + 1, size: level.entries.length });
    if (open) walk(tree, root, path, depth + 1, rows);
  });
  if (level.error) rows.push({ type: "more", key: `more:${key}:error`, level: key, root, dir, depth, state: "error", text: level.error });
  else if (level.next) rows.push({ type: "more", key: `more:${key}:${level.next}`, level: key, root, dir, depth, state: level.loading ? "loading" : "idle", text: "" });
}

/** The keys of the open folders that are under (or are) a root: what a root going away closes. */
export function openUnder(tree, root) {
  return Object.keys(tree.expanded).filter((k) => k.startsWith(`${root}:`));
}
