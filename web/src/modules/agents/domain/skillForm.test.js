import { assert, file, test } from "../../../shared/testing/test.js";
import {
  DESCRIPTION_LIMIT, START_FROM, checkSkillDraft, coachDescription, decodeUpload, descriptionMeter, normalizeNewPath,
  normalizeUploadPath, previewModel, reachOf, renameNote, returnHref, safeReturn, skillChanges, skillDraft,
  skillFieldErrors, skillSave, startFromBody,
} from "./skillForm.js";
import { describeRefusal } from "./refusals.js";
import { StructuredError } from "../../../shared/domain/errors.js";

file("agents/domain/skillForm");

const NAMES = ["tdd", "tdd-go", "lint", "copy", "figma-handoff"];

const catalog = {
  agents: [
    { name: "designer", entries: ["@ux", "lint"] },
    { name: "ux-designer", entries: ["@ux"] },
    { name: "backend", entries: ["tdd"] },
  ],
  bundles: [
    { name: "ux", entries: ["lint", "@laws"], usedByAgents: ["designer", "ux-designer"], usedByBundles: ["all"] },
    { name: "laws", entries: ["copy"], usedByAgents: [], usedByBundles: ["ux"] },
    { name: "all", entries: ["@ux", "tdd"], usedByAgents: ["backend"], usedByBundles: [] },
  ],
  skills: NAMES.map((name) => ({ name, description: `${name} skill.`, agents: name === "lint" ? ["designer"] : [], bundles: name === "lint" ? ["ux"] : [] })),
};

const detail = {
  name: "lint",
  description: "Lints. Use when code changes.",
  body: "## Steps\n1. Run it.",
  files: [
    { path: "references", dir: true, size: 0, content: "", encoding: "utf8" },
    { path: "references/rules.md", dir: false, size: 5, content: "rules", encoding: "utf8" },
    { path: "assets/logo.png", dir: false, size: 4, content: "AAEC", encoding: "base64" },
  ],
  agents: ["designer"],
  bundles: ["ux"],
  agentCount: 2,
};

test("the coach checks that a description says what it does, when to use it and points to a neighbour", () => {
  const good = "Turn a Figma frame into specs developers can build from. Use when a design is ready for implementation. For reviewing a built screen, use `tdd`.";
  const c = coachDescription(good, { names: NAMES, self: "figma-handoff" });
  assert.deepEqual(c.map((x) => [x.id, x.ok]), [["what", true], ["when", true], ["neighbour", true]]);
  assert.equal(c[0].label, "Says what it does in the first sentence");
  assert.equal(c[1].label, "Says when to use it (“Use when…”)");
  assert.equal(c[2].label, "Points to a neighbour skill, so the two are not confused");

  const vague = coachDescription("Testing stuff", { names: NAMES });
  assert.deepEqual(vague.map((x) => x.ok), [false, false, false]);
  assert.deepEqual(coachDescription("", { names: NAMES }).map((x) => x.ok), [false, false, false]);

  const onlyWhat = coachDescription("Writes specs from design frames.", { names: NAMES });
  assert.deepEqual(onlyWhat.map((x) => x.ok), [true, false, false]);
  const whenOnly = coachDescription("Writes specs from frames. use  WHEN a design is ready.", { names: NAMES });
  assert.deepEqual(whenOnly.map((x) => x.ok), [true, true, false]);
});

test("a neighbour is a real other skill named distinctively, never the skill itself or a common word", () => {
  const nb = (text, self = "") => coachDescription(`Does a thing for you. ${text}`, { names: NAMES, self })[2].ok;
  assert.equal(nb("For tests use tdd-go instead."), true, "a hyphenated name stands out");
  assert.equal(nb("See `lint`."), true, "backticks mark a name");
  assert.equal(nb("Prefer lint for that."), true, "after a pointing word");
  assert.equal(nb("Please copy the file."), false, "copy here is just a word");
  assert.equal(nb("Use `tdd-go`.", "tdd-go"), false, "itself is not a neighbour");
  assert.equal(nb("Use `unknown-skill`."), false, "a skill the catalog lacks is not a neighbour");
});

test("the meter counts characters against 1024 and says when it is over", () => {
  assert.equal(DESCRIPTION_LIMIT, 1024);
  assert.deepEqual(descriptionMeter("x".repeat(231)), { count: 231, limit: 1024, over: false, percent: 22.55859375, text: "231 / 1024" });
  const over = descriptionMeter("x".repeat(1030));
  assert.equal(over.over, true);
  assert.equal(over.percent, 100);
  assert.equal(descriptionMeter("é😀").count, 2, "characters, not bytes or UTF-16 units");
});

test("Start from offers Blank, Workflow, Reference and a copy; the templates are Markdown with steps or rules", () => {
  assert.deepEqual(START_FROM.map((s) => s.id), ["blank", "workflow", "reference", "copy"]);
  assert.equal(startFromBody("blank"), "");
  assert.ok(startFromBody("workflow").includes("## Steps"));
  assert.ok(startFromBody("workflow").includes("## Checks"));
  assert.ok(startFromBody("reference").includes("## Rules"));
  assert.ok(startFromBody("reference").includes("## Examples"));
  assert.equal(startFromBody("copy"), "", "a copy brings another skill's body, not a template");
});

test("a new skill starts empty, takes a name from the link, and a copy brings the other skill's text and files but no readers", () => {
  const empty = skillDraft(catalog, { name: "figma" });
  assert.equal(empty.previousName, "");
  assert.equal(empty.name, "figma");
  assert.deepEqual([empty.description, empty.body, empty.files, empty.bundles, empty.agents], ["", "", [], [], []]);

  const copy = skillDraft(catalog, { from: detail });
  assert.equal(copy.name, "", "the copy has no name until the person gives one");
  assert.equal(copy.description, detail.description);
  assert.equal(copy.body, detail.body);
  assert.deepEqual(copy.files.map((f) => [f.path, f.encoding, f.origin]), [["references/rules.md", "utf8", "added"], ["assets/logo.png", "base64", "added"]]);
  assert.deepEqual([copy.bundles, copy.agents], [[], []]);
});

test("editing opens with the skill as the server read it: folders skipped, readers from the catalog", () => {
  const d = skillDraft(catalog, { previous: detail });
  assert.equal(d.previousName, "lint");
  assert.equal(d.name, "lint");
  assert.deepEqual(d.files.map((f) => [f.path, f.origin]), [["references/rules.md", "existing"], ["assets/logo.png", "existing"]]);
  assert.deepEqual([d.bundles, d.agents], [["ux"], ["designer"]]);
  assert.equal(skillChanges(d), 0);
});

test("changes count each field once, each file added, edited or removed, and each reader added or removed", () => {
  const d = skillDraft(catalog, { previous: detail });
  d.description = "New.";
  d.body = "Changed";
  assert.equal(skillChanges(d), 2);
  d.files[0].content = "rules!";
  d.files.push({ path: "new.md", content: "n", encoding: "utf8", size: 1, origin: "added", original: null });
  d.files.splice(1, 1);
  d.deleted.push("assets/logo.png");
  assert.equal(skillChanges(d), 2 + 3);
  d.agents.push("backend");
  d.bundles = [];
  assert.equal(skillChanges(d), 2 + 3 + 2);

  const fresh = skillDraft(catalog, {});
  assert.equal(skillChanges(fresh), 0);
  fresh.name = "x";
  assert.equal(skillChanges(fresh), 1);
});

test("the form is checked at once: a slug, a description of at most 1024 characters, a name nothing else has", () => {
  const ok = (o) => checkSkillDraft({ ...skillDraft(catalog, {}), name: "figma", description: "Does it.", ...o }, catalog, { touched: true });
  assert.equal(ok({}).valid, true);
  assert.equal(ok({ name: "Figma Handoff" }).valid, false);
  assert.equal(ok({ name: "Figma Handoff" }).nameProblem, "Skill name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
  assert.equal(ok({ name: "" }).valid, false);
  assert.equal(ok({ name: "" }).nameProblem, "", "an empty name is not scolded before it is typed");

  assert.equal(ok({ description: "  " }).valid, false);
  assert.equal(ok({ description: "  " }).descriptionProblem, "Add a description to save.");
  assert.equal(checkSkillDraft({ ...skillDraft(catalog, {}), name: "figma" }, catalog, { touched: false }).descriptionProblem, "", "a new form does not scold");

  const long = ok({ description: "x".repeat(1030) });
  assert.equal(long.valid, false);
  assert.equal(long.descriptionProblem, "The description is 1030 characters, 6 over the limit of 1024. Agents read all of it, so cut it down.");
  assert.equal(ok({ description: "x".repeat(1024) }).valid, true, "exactly 1024 fits");
});

test("a taken name is refused with a link to the skill and a free name to try; renaming to its own name is fine", () => {
  const t = checkSkillDraft({ ...skillDraft(catalog, {}), name: "tdd", description: "Does it." }, catalog, { touched: true });
  assert.equal(t.valid, false);
  assert.deepEqual(t.taken, { name: "tdd", href: "/agents/skills/tdd", suggestion: "tdd-2" });
  assert.equal(t.nameProblem, "A skill named tdd already exists.");

  const free = checkSkillDraft({ ...skillDraft(catalog, {}), name: "tdd-2", description: "d" }, catalog, { touched: true });
  assert.equal(free.taken, null);
  const own = checkSkillDraft({ ...skillDraft(catalog, { previous: detail }), description: "Does it." }, catalog, { touched: true });
  assert.equal(own.taken, null);
  const toOther = checkSkillDraft({ ...skillDraft(catalog, { previous: detail }), name: "copy" }, catalog, { touched: true });
  assert.equal(toOther.taken.name, "copy");
  assert.equal(toOther.taken.suggestion, "copy-2");
});

test("a rename says how many places in agents.json name the skill; none, or no rename, says nothing", () => {
  const c = { ...catalog, agents: [...catalog.agents], bundles: catalog.bundles };
  assert.deepEqual(renameNote(c, "lint", "linter"), { show: true, places: 2, text: "Renaming moves the folder and rewrites the 2 places in agents.json that name it. Running sessions keep the old copy until they end." });
  assert.equal(renameNote(c, "lint", "lint").show, false);
  assert.equal(renameNote(c, "", "x").show, false);
  assert.equal(renameNote(c, "lint", "").show, false, "an empty new name is not a rename yet");
  assert.equal(renameNote(c, "figma-handoff", "figma").text, "Renaming moves the folder. Nothing in agents.json names it, so no references change.");
  assert.equal(renameNote({ ...c, agents: [{ name: "a", entries: ["tdd"] }], bundles: [] }, "tdd", "t").text, "Renaming moves the folder and rewrites the 1 place in agents.json that names it. Running sessions keep the old copy until they end.");
});

test("who gets it follows the bundles through the bundles that hold them, and an empty choice keeps it on the shelf", () => {
  const draft = (bundles, agents) => ({ bundles, agents });
  assert.deepEqual(reachOf(catalog, draft(["ux"], [])).agents, ["backend", "designer", "ux-designer"], "@ux reaches its agents and, through @all, backend");
  assert.deepEqual(reachOf(catalog, draft(["laws"], ["backend"])).agents, ["backend", "designer", "ux-designer"], "@laws is held by @ux");
  assert.equal(reachOf(catalog, draft([], ["designer", "backend"])).text, "Reaches 2 agents: backend, designer. Leave both empty to keep the skill on the shelf.");
  assert.equal(reachOf(catalog, draft([], ["designer"])).text, "Reaches 1 agent: designer. Leave both empty to keep the skill on the shelf.");
  assert.equal(reachOf(catalog, draft([], [])).text, "Reaches no agent: the skill stays on the shelf.");
  assert.equal(reachOf(catalog, draft([], [])).none, true);
  const many = { ...catalog, agents: Array.from({ length: 9 }, (_, i) => ({ name: `a${i}`, entries: [] })), bundles: [] };
  assert.equal(reachOf(many, draft([], many.agents.map((a) => a.name))).text, "Reaches 9 agents: a0, a1, a2, a3, a4, a5 and 3 more. Leave both empty to keep the skill on the shelf.");
});

test("what a save sends: only new and edited files, the removed ones, and the full list of readers", () => {
  const d = skillDraft(catalog, { previous: detail });
  assert.deepEqual(skillSave(d).skill.files, [], "nothing changed, nothing to send");
  d.files[0].content = "rules!";
  d.files.push({ path: "n.md", content: "n", encoding: "utf8", size: 1, origin: "added", original: null });
  d.deleted.push("gone.md");
  d.description = "  Trimmed.  ";
  const s = skillSave(d);
  assert.equal(s.previousName, "lint");
  assert.equal(s.skill.description, "Trimmed.");
  assert.deepEqual(s.skill.files, [{ path: "references/rules.md", content: "rules!", encoding: "utf8" }, { path: "n.md", content: "n", encoding: "utf8" }]);
  assert.deepEqual(s.skill.deletedFiles, ["gone.md"]);
  assert.deepEqual(s.addTo, { agents: ["designer"], bundles: ["ux"] });
  assert.equal(skillSave(skillDraft(catalog, { name: "x" })).previousName, undefined, "a new skill has no previous name");
});

test("a folder upload loses its top folder, backslashes and noise; a path that leaves the skill is dropped", () => {
  assert.equal(normalizeUploadPath("mytool/references/a.md", { folder: true }), "references/a.md");
  assert.equal(normalizeUploadPath("mytool\\scripts\\run.sh", { folder: true }), "scripts/run.sh");
  assert.equal(normalizeUploadPath("mytool/./a.md", { folder: true }), "a.md");
  assert.equal(normalizeUploadPath("mytool/SKILL.md", { folder: true }), null, "the body is written from the form");
  assert.equal(normalizeUploadPath("mytool/.git/config", { folder: true }), null);
  assert.equal(normalizeUploadPath("mytool/.DS_Store", { folder: true }), null);
  assert.equal(normalizeUploadPath("mytool/a/../../x", { folder: true }), null);
  assert.equal(normalizeUploadPath("mytool", { folder: true }), null, "a folder with nothing in it");
  assert.equal(normalizeUploadPath("notes.md", { folder: false }), "notes.md", "a loose file keeps its name");
  assert.equal(normalizeUploadPath("/etc/passwd", { folder: false }), "etc/passwd");
  assert.equal(normalizeUploadPath("SKILL.md", { folder: false }), null);
  assert.equal(normalizeUploadPath("a//b.md", { folder: false }), "a/b.md");
  assert.equal(normalizeUploadPath("", { folder: false }), null);
});

test("a typed file name is cleaned the same way and says what is wrong", () => {
  assert.deepEqual(normalizeNewPath("references/checklist.md"), { path: "references/checklist.md", problem: "" });
  assert.deepEqual(normalizeNewPath("  /scripts//run.sh "), { path: "scripts/run.sh", problem: "" });
  assert.equal(normalizeNewPath("").problem, "Name the file, for example references/checklist.md.");
  assert.equal(normalizeNewPath("../x").problem, "A file stays inside the skill's folder: no “..”.");
  assert.equal(normalizeNewPath("SKILL.md").problem, "SKILL.md is written from the form. Pick another name.");
});

test("an upload is text when it decodes as UTF-8 and base64 when it does not", () => {
  assert.deepEqual(decodeUpload(new TextEncoder().encode("héllo\n")), { content: "héllo\n", encoding: "utf8" });
  assert.deepEqual(decodeUpload(new Uint8Array([0, 1, 2, 255])), { content: "AAEC/w==", encoding: "base64" });
  assert.deepEqual(decodeUpload(new Uint8Array([])), { content: "", encoding: "utf8" });
  assert.equal(decodeUpload(new Uint8Array([104, 0, 105])).encoding, "base64", "a NUL byte is binary");
});

test("the preview shows a new file as the server wrote it, and an edited file as a diff", () => {
  const m = previewModel([
    { path: "skills/figma/SKILL.md", before: null, after: "---\nname: figma\n---\nbody\n" },
    { path: "agents.json", before: '{\n  "a": 1\n}\n', after: '{\n  "a": 2\n}\n' },
    { path: "skills/figma/logo.png", before: null, after: "AAEC", encoding: "base64" },
    { path: "skills/old/SKILL.md", before: "x\n", after: null },
  ]);
  assert.deepEqual(m.map((f) => [f.path, f.badge, f.isText]), [
    ["skills/figma/SKILL.md", "new", true],
    ["agents.json", "edit", false],
    ["skills/figma/logo.png", "new", true],
    ["skills/old/SKILL.md", "deleted", false],
  ]);
  assert.equal(m[0].text, "---\nname: figma\n---\nbody\n", "verbatim, never rebuilt");
  assert.deepEqual(m[1].lines.filter((l) => l.kind !== "same").map((l) => l.mark + l.text), ['−  "a": 1', '+  "a": 2']);
  assert.equal(m[2].text, "binary file · 3 bytes");
  assert.equal(m[3].lines[0].mark, "−");
});

test("the server's refusals land on the name, the description, the files or the form", () => {
  const err = (code, message, details = {}) => describeRefusal(new StructuredError(code, message, 400, details));
  assert.equal(skillFieldErrors(null).name, "");
  const taken = skillFieldErrors(err("NAME_TAKEN", "exists", { suggestion: "tdd-2" }));
  assert.equal(taken.name, "That name is taken. Try tdd-2.");
  assert.equal(taken.suggestion, "tdd-2");
  assert.equal(skillFieldErrors(err("INVALID_INPUT", "description is required", { field: "description" })).description, "description is required");
  assert.equal(skillFieldErrors(err("INVALID_INPUT", "bad path", { field: "files" })).files, "bad path");
  assert.equal(skillFieldErrors(err("INVALID_INPUT", "bad", { field: "skill.name" })).name, "bad");
  assert.equal(skillFieldErrors(err("UNKNOWN_REFERENCE", "x", { names: ["ghost"] })).form, "These do not exist: ghost.");
  assert.equal(skillFieldErrors(err("CATALOG_BROKEN", "agents.json is broken")).form, "agents.json is broken");
});

test("the way back is a path of this site, with the added skill before the #fragment", () => {
  assert.equal(safeReturn("/agents/new#skills"), "/agents/new#skills");
  assert.equal(safeReturn("/agents/x/edit?from=y#skills"), "/agents/x/edit?from=y#skills");
  assert.equal(safeReturn("https://evil.test/x"), "");
  assert.equal(safeReturn("//evil.test/x"), "");
  assert.equal(safeReturn("/\\evil.test"), "");
  assert.equal(safeReturn("javascript:alert(1)"), "");
  assert.equal(safeReturn(""), "");
  assert.equal(safeReturn(null), "");

  assert.equal(returnHref("/agents/new#skills", "figma"), "/agents/new?added=figma#skills");
  assert.equal(returnHref("/agents/x/edit?from=y#skills", "figma"), "/agents/x/edit?from=y&added=figma#skills");
  assert.equal(returnHref("/agents/new", "a b"), "/agents/new?added=a%20b");
  assert.equal(returnHref("/agents/new?added=old#skills", "figma"), "/agents/new?added=figma#skills", "an old added is replaced");
  assert.equal(returnHref("/agents/new#skills", ""), "/agents/new#skills", "cancel adds nothing");
});
