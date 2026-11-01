import { assert, file, test } from "../../../shared/testing/test.js";
import {
  decideOnFeed,
  fieldErrorsOf,
  fieldMessages,
  groupFiles,
  isDirty,
  isStale,
  listItems,
  lineDiff,
  mergeMetadata,
  pathAfterKindChange,
  prefillPath,
  recordKindOf,
  refusalText,
} from "./instructionFiles.js";

file("architecture/domain/instructionFiles");

test("a new file's path is prefilled from its kind", () => {
  assert.equal(prefillPath("memory"), "CLAUDE.md");
  assert.equal(prefillPath("skill", "go"), ".claude/skills/go/SKILL.md");
  assert.equal(prefillPath("subagent", "reviewer"), ".claude/agents/reviewer.md");
  assert.equal(prefillPath("skill"), ".claude/skills/<name>/SKILL.md");
  assert.equal(prefillPath("subagent", "  "), ".claude/agents/<name>.md");
  assert.equal(prefillPath("other"), "");
});

test("the file kinds map to the record's kinds and back to words", () => {
  assert.equal(recordKindOf("memory"), "rules");
  assert.equal(recordKindOf("skill"), "skill");
  assert.equal(recordKindOf("subagent"), "subagent_prompt");
  assert.equal(recordKindOf("weird"), "weird");
});

test("files group into plain files and skill folders, in path order", () => {
  const files = [
    { path: ".claude/skills/go/notes.md", kind: "skill" },
    { path: "CLAUDE.md", kind: "memory" },
    { path: ".claude/skills/go/SKILL.md", kind: "skill" },
    { path: ".claude/agents/reviewer.md", kind: "subagent" },
    { path: ".claude/skills/web/SKILL.md", kind: "skill" },
  ];
  const groups = groupFiles(files);
  assert.deepEqual(
    groups.map((g) => (g.type === "skill" ? `skill ${g.folder} [${g.files.map((f) => f.path).join(", ")}]` : `file ${g.file.path}`)),
    [
      "file .claude/agents/reviewer.md",
      "skill .claude/skills/go [.claude/skills/go/SKILL.md, .claude/skills/go/notes.md]",
      "skill .claude/skills/web [.claude/skills/web/SKILL.md]",
      "file CLAUDE.md",
    ],
  );
  assert.equal(groups[1].name, "go");
});

test("a skill file outside a skill folder stays a plain file", () => {
  const groups = groupFiles([{ path: "SKILL.md", kind: "skill" }]);
  assert.deepEqual(groups.map((g) => g.type), ["file"]);
});

test("file metadata merges with the record's entries; a file with no entry gets the defaults", () => {
  const files = [
    { path: "CLAUDE.md", kind: "memory", version: 3, size: 20 },
    { path: ".claude/agents/reviewer.md", kind: "subagent", version: 1, size: 9 },
  ];
  const entries = [
    { path: "CLAUDE.md", kind: "rules", applies_to: ["go-developer"], loaded: "on_demand", note: "Rules." },
    { path: "docs/old.md", kind: "rules", applies_to: ["all"], loaded: "always" },
  ];
  const rows = mergeMetadata(files, entries);
  assert.deepEqual(
    rows.map((r) => [r.path, r.kind, r.applies_to, r.loaded, r.note ?? "", r.file?.version ?? null, r.recorded]),
    [
      ["CLAUDE.md", "rules", ["go-developer"], "on_demand", "Rules.", 3, true],
      ["docs/old.md", "rules", ["all"], "always", "", null, true],
      [".claude/agents/reviewer.md", "subagent_prompt", ["all"], "always", "", 1, false],
    ],
  );
});

test("dirty is a difference between the text and what was loaded", () => {
  assert.equal(isDirty("a", "a"), false);
  assert.equal(isDirty("a", "a "), true);
  assert.equal(isDirty(null, ""), false);
  assert.equal(isDirty(undefined, "x"), true);
});

test("a save refused with INSTRUCTION_CHANGED is stale", () => {
  assert.equal(isStale({ code: "INSTRUCTION_CHANGED" }), true);
  assert.equal(isStale({ code: "INSTRUCTION_INVALID" }), false);
  assert.equal(isStale(null), false);
});

test("a feed event reloads a clean file, warns on an edited one, and ignores what is not ours", () => {
  const open = { path: "CLAUDE.md", version: 3, dirty: false };
  const change = (p, v) => ({ path: p, version: v });
  assert.equal(decideOnFeed(open, change("CLAUDE.md", 4)), "reload");
  assert.equal(decideOnFeed({ ...open, dirty: true }, change("CLAUDE.md", 4)), "notice");
  assert.equal(decideOnFeed(open, change("CLAUDE.md", 3)), "ignore");
  assert.equal(decideOnFeed(open, change("other.md", 9)), "ignore");
  assert.equal(decideOnFeed(null, change("CLAUDE.md", 4)), "ignore");
  assert.equal(decideOnFeed(open, change("CLAUDE.md", 0)), "removed");
  assert.equal(decideOnFeed({ ...open, dirty: true }, change("CLAUDE.md", 0)), "removed");
});

test("each refusal code has one sentence; an unknown code has none", () => {
  const codes = [
    "CHECKOUT_DIRTY",
    "WRONG_BRANCH",
    "INSTRUCTION_NOT_IMPORTED",
    "INSTRUCTION_NOT_FOUND",
    "INSTRUCTION_CHANGED",
    "INSTRUCTION_EXISTS",
    "INSTRUCTION_INVALID",
    "REPOSITORY_NOT_FOUND",
  ];
  const seen = new Set();
  for (const code of codes) {
    const text = refusalText(code);
    assert.ok(text.endsWith("."), `${code} has a sentence`);
    assert.equal(text.includes("\n"), false);
    seen.add(text);
  }
  assert.equal(seen.size, codes.length);
  assert.ok(refusalText("CHECKOUT_DIRTY").includes("uncommitted"));
  assert.ok(refusalText("WRONG_BRANCH").includes("default branch"));
  assert.ok(refusalText("INSTRUCTION_NOT_IMPORTED").includes("Import"));
  assert.equal(refusalText("NOPE"), "");
});

test("INSTRUCTION_INVALID's details become one error per field", () => {
  const details = { errors: [{ path: "/path", code: "ABSOLUTE_PATH", message: "Use a path inside the repository." }, { nope: 1 }, { path: "/content", message: "Too large." }] };
  assert.deepEqual(fieldErrorsOf(details), [
    { path: "/path", code: "ABSOLUTE_PATH", message: "Use a path inside the repository." },
    { path: "/content", code: "", message: "Too large." },
  ]);
  assert.deepEqual(fieldErrorsOf(undefined), []);
});

test("field errors of an INSTRUCTION_INVALID refusal land on the path and the text", () => {
  const errors = [
    { path: "/path", code: "PATH_ESCAPES", message: "A path cannot contain .." },
    { path: "/content", code: "TOO_LARGE", message: "A file can be at most 256 KB." },
    { path: "/path", code: "OTHER", message: "second" },
    { path: "/kind", code: "X", message: "kind" },
  ];
  assert.deepEqual(fieldMessages(errors), { path: "A path cannot contain ..", content: "A file can be at most 256 KB.", other: "kind" });
  assert.deepEqual(fieldMessages([]), { path: "", content: "", other: "" });
});

test("changing the kind of a new file re-prefills the path only while the person has not typed their own", () => {
  assert.equal(pathAfterKindChange("memory", "CLAUDE.md", "skill"), ".claude/skills/<name>/SKILL.md");
  assert.equal(pathAfterKindChange("skill", ".claude/skills/<name>/SKILL.md", "subagent"), ".claude/agents/<name>.md");
  assert.equal(pathAfterKindChange("memory", "", "subagent"), ".claude/agents/<name>.md");
  assert.equal(pathAfterKindChange("skill", ".claude/skills/go/SKILL.md", "memory"), ".claude/skills/go/SKILL.md");
});

test("a line diff marks what the first text has alone, what the second has alone, and what they share", () => {
  assert.deepEqual(lineDiff("a\nb\nc", "a\nB\nc"), [
    { type: "eq", text: "a" },
    { type: "del", text: "b" },
    { type: "ins", text: "B" },
    { type: "eq", text: "c" },
  ]);
  assert.deepEqual(lineDiff("", "x\ny"), [
    { type: "ins", text: "x" },
    { type: "ins", text: "y" },
  ]);
  assert.deepEqual(lineDiff("x\n", "x\n"), [{ type: "eq", text: "x" }]);
  assert.deepEqual(lineDiff("", ""), []);
  assert.deepEqual(lineDiff("a\nb", "b\nc").map((l) => l.type), ["del", "eq", "ins"]);
});

test("a diff of huge texts falls back to replacing the whole text", () => {
  const big = (n) => Array.from({ length: n }, (_, i) => `line ${i}`).join("\n");
  const d = lineDiff(big(3000), big(3000).replaceAll("line", "LINE"));
  assert.equal(d.filter((l) => l.type === "del").length, 3000);
  assert.equal(d.filter((l) => l.type === "ins").length, 3000);
});

test("the list draws a skill as a folder header before its files, keeping the rows in their order", () => {
  const rows = [
    { path: "CLAUDE.md", kind: "rules", file: { kind: "memory" } },
    { path: ".claude/skills/go/notes.md", kind: "skill", file: { kind: "skill" } },
    { path: ".claude/skills/go/SKILL.md", kind: "skill", file: { kind: "skill" } },
    { path: "docs/orphan.md", kind: "rules", file: null },
    { path: ".claude/skills/web/SKILL.md", kind: "skill", file: null },
  ];
  const items = listItems(rows);
  assert.deepEqual(
    items.map((i) => (i.folder ? `folder ${i.folderPath} ${i.name} ${i.count}` : `${i.index} ${i.row.path} ${i.inSkill}`)),
    [
      "0 CLAUDE.md false",
      "folder .claude/skills/go go 2",
      "2 .claude/skills/go/SKILL.md true",
      "1 .claude/skills/go/notes.md true",
      "3 docs/orphan.md false",
      "folder .claude/skills/web web 1",
      "4 .claude/skills/web/SKILL.md true",
    ],
  );
  assert.equal(items[0].entry, "instruction");
  assert.equal(items[1].entry, null);
});

test("a rename or a delete has words for each refusal", () => {
  assert.ok(refusalText("INSTRUCTION_EXISTS").includes("already exists"));
  assert.ok(refusalText("INSTRUCTION_CHANGED").includes("newer version"));
});

test("a skill folder with no SKILL.md the harness holds is marked as not delivered", () => {
  const held = (path) => ({ path, kind: "skill", file: { kind: "skill", path } });
  const items = listItems([
    held(".claude/skills/solo/notes.md"),
    held(".claude/skills/ok/SKILL.md"),
    held(".claude/skills/ok/notes.md"),
    { path: ".claude/skills/recorded/SKILL.md", kind: "skill", file: null },
    held(".claude/skills/deep/sub/SKILL.md"),
  ]);
  const folders = items.filter((i) => i.folder);
  assert.deepEqual(
    folders.map((f) => `${f.name} ${f.needsSkillMd}`),
    ["solo true", "ok false", "recorded true", "deep true"],
  );
});
