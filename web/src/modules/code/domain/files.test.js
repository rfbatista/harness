import { assert, file, test } from "../../../shared/testing/test.js";
import { agentWords, emptyFilesTree, filesRows, levelKey, newLevel, openUnder } from "./files.js";

file("code/domain/files");

const roots = [
  { root: "worktree", label: "Worktree", available: true, note: "" },
  { root: "resources", label: "Resources", available: false, note: "The resources folder ended with the session." },
];
const entry = (name, kind = "file") => ({ name, kind, ignored: false, size: 1, target: "" });
const withRoots = () => ({ ...emptyFilesTree(), roots });

test("both roots are listed in order, an unavailable one with its note and no children", () => {
  const rows = filesRows(withRoots());
  assert.deepEqual(rows.map((r) => r.type), ["root", "root", "note"]);
  assert.equal(rows[2].text, "The resources folder ended with the session.");
});

test("an open root shows skeletons until its first page, then its entries and a paging row while more remain", () => {
  const tree = { ...withRoots(), expanded: { [levelKey("worktree", "")]: true }, levels: {} };
  assert.equal(filesRows(tree).filter((r) => r.type === "skeleton").length, 3);
  tree.levels[levelKey("worktree", "")] = { ...newLevel(), entries: [entry("a", "dir"), entry("b")], next: "b" };
  const rows = filesRows(tree);
  assert.deepEqual(rows.map((r) => r.type), ["root", "entry", "entry", "more", "root", "note"]);
  assert.equal(rows[3].state, "idle");
  tree.levels[levelKey("worktree", "")].error = "boom";
  assert.equal(filesRows(tree)[3].state, "error");
});

test("a folder's entries sit one level deeper under it, and closing a root closes what is under it", () => {
  const tree = {
    ...withRoots(),
    expanded: { [levelKey("worktree", "")]: true, [levelKey("worktree", "a")]: true },
    levels: {
      [levelKey("worktree", "")]: { ...newLevel(), entries: [entry("a", "dir")] },
      [levelKey("worktree", "a")]: { ...newLevel(), entries: [entry("x")] },
    },
  };
  const rows = filesRows(tree);
  assert.equal(rows[2].path, "a/x");
  assert.equal(rows[2].depth, 2);
  assert.equal(openUnder(tree, "worktree").length, 2);
});

test("the strip words follow the agent state", () => {
  assert.equal(agentWords("working").status, "running");
  assert.equal(agentWords("waiting").status, "waiting");
  assert.equal(agentWords("idle").status, "idle");
  assert.equal(agentWords("ended").status, "done");
});
