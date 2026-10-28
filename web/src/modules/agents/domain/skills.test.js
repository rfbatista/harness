import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import { SKILL_PAGE, skillFiles, skillRows, skillsFooter, skillSummary } from "./skills.js";

file("agents/domain/skills");

const many = (n) => {
  const dto = catalogDTO();
  dto.skills = Array.from({ length: n }, (_, i) => ({
    name: `skill-${String(i).padStart(3, "0")}`, description: i % 2 ? "Odd one." : "Even one.", file_count: 1, has_extra_files: i % 10 === 0,
    agents: [], bundles: i % 5 === 0 ? ["ux"] : [], agent_count: i % 3 === 0 ? 0 : 2, problems: [],
  }));
  return toCatalog(dto);
};

test("a skill row shows its name, description and how many agents it reaches", () => {
  const [tdd, lint] = skillRows(toCatalog(catalogDTO())).rows;
  assert.equal(tdd.name, "tdd");
  assert.equal(tdd.href, "/agents/skills/tdd");
  assert.equal(tdd.description, "Test first.");
  assert.equal(tdd.meta, "reaches 2 agents");
  assert.equal(lint.meta, "reaches 1 agent");
});

test("a skill nobody reaches says unused; one with no description wears the badge", () => {
  const dto = catalogDTO();
  dto.skills.push({ name: "lonely", description: "", file_count: 1, has_extra_files: false, agents: [], bundles: [], agent_count: 0, problems: [{ code: "NO_DESCRIPTION", message: "x" }] });
  const row = skillRows(toCatalog(dto)).rows.find((r) => r.name === "lonely");
  assert.equal(row.meta, "unused");
  assert.equal(row.badge.label, "No description");
});

test("the quick filters narrow the list: Unused, In a bundle, Has extra files", () => {
  const c = many(30);
  const names = (filter) => skillRows(c, { filter }).rows.map((r) => r.name);
  assert.equal(names("unused").length, 10);
  assert.ok(names("unused").every((n) => Number(n.slice(-3)) % 3 === 0));
  assert.deepEqual(names("bundle"), ["skill-000", "skill-005", "skill-010", "skill-015", "skill-020", "skill-025"]);
  assert.deepEqual(names("extra"), ["skill-000", "skill-010", "skill-020"]);
});

test("the text filter matches the name or the description, ignoring case", () => {
  const c = many(10);
  assert.equal(skillRows(c, { query: "ODD" }).total, 5);
  assert.deepEqual(skillRows(c, { query: "skill-007" }).rows.map((r) => r.name), ["skill-007"]);
});

test("a list of 475 skills hands out one page of rows at a time and says how many more wait", () => {
  const c = many(475);
  const first = skillRows(c);
  assert.equal(first.rows.length, SKILL_PAGE);
  assert.ok(SKILL_PAGE <= 100);
  assert.equal(first.total, 475);
  assert.equal(first.hidden, 475 - SKILL_PAGE);
  const all = skillRows(c, { limit: 475 });
  assert.equal(all.rows.length, 475);
  assert.equal(all.hidden, 0);
});

test("the footer names the folder count", () => {
  assert.equal(skillsFooter(many(475)), "skills/ · 475 folders");
  assert.equal(skillsFooter(many(1)), "skills/ · 1 folder");
});

test("a skill's summary from the list: path, reach and the Edit and Duplicate links", () => {
  const s = skillSummary(toCatalog(catalogDTO()), "tdd");
  assert.equal(s.path, "skills/tdd/SKILL.md");
  assert.equal(s.copyPath, "/data/agents/skills/tdd");
  assert.equal(s.editHref, "/agents/skills/tdd/edit");
  assert.equal(s.duplicateHref, "/agents/skills/new?from=tdd");
  assert.equal(skillSummary(toCatalog(catalogDTO()), "ghost"), null);
});

test("the file tree lists folders before files, indented by depth, SKILL.md first", () => {
  const rows = skillFiles([
    { path: "refs/b.md", dir: false, size: 10 },
    { path: "refs", dir: true, size: 0 },
    { path: "mocking.md", dir: false, size: 2048 },
    { path: "refs/deep", dir: true, size: 0 },
    { path: "refs/deep/c.md", dir: false, size: 1 },
  ]);
  assert.deepEqual(rows.map((r) => `${r.depth}:${r.name}${r.dir ? "/" : ""}`), ["0:SKILL.md", "0:refs/", "1:deep/", "2:c.md", "1:b.md", "0:mocking.md"]);
  assert.equal(rows[5].size, "2 KB");
});

test("a skill with no extra files lists only SKILL.md", () => {
  assert.deepEqual(skillFiles([]).map((r) => r.name), ["SKILL.md"]);
});
