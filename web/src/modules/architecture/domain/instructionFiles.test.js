import { assert, file, test } from "../../../shared/testing/test.js";
import {
  decideOnFeed,
  fieldErrorsOf,
  groupFiles,
  isDirty,
  isStale,
  mergeMetadata,
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
