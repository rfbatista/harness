import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/skillEditor");

function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    async fire() {
      const fns = [...pending.values()];
      pending.clear();
      for (const fn of fns) await fn();
    },
  };
}
const settle = () => new Promise((r) => setTimeout(r, 0));

/** The page at `path`, over the memory catalog. */
async function open(path, { search = "" } = {}) {
  const world = makeWorld();
  world.catalog.bundles.push({ name: "extra", entries: ["tdd"], resolved_skills: ["tdd"], used_by_agents: [], used_by_bundles: [] });
  const { gateway, server } = memoryCatalog(world);
  const timers = fakeTimers();
  const pushed = [];
  const location = { pathname: path, search };
  const history = { pushState: (_s, _t, url) => { pushed.push(url); location.pathname = url.split(/[?#]/)[0]; location.search = ""; } };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history, location, timers }), { el: seededElement(seed) });
  await m.instance.init();
  await settle();
  return { ...m, server, timers, pushed, location, page: m.instance };
}

const type = (page, field, value) => { page.sfDraft[field] = value; page.sfChanged(); };
const saves = (server, dry) => server.requests.filter(([m, p, b]) => m === "POST" && p === "/save_catalog_skill" && Boolean(b.dry_run) === dry);
const ev = (dataset = {}, extra = {}) => ({ currentTarget: { dataset, ...extra }, preventDefault() {}, ...extra });
const GOOD = "Turns a Figma frame into specs. Use when a design is ready. For checking a built screen, use tdd.";

test("the skill form takes the page: the header and tabs give way, an empty form says nothing is written yet", async () => {
  const { page } = await open("/agents/skills/new");
  assert.equal(page.skillEditing, true);
  assert.equal(page.fullPage, true);
  assert.equal(page.agentEditing, false);
  assert.equal(page.skillTitle, "New skill");
  assert.equal(page.saveButtonLabel, "Create skill");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Nothing is written until you create the skill.");
  assert.equal(page.fieldErrors.description, "", "a new form does not scold");
  assert.equal(page.sfPreviewHint, "Give the skill a name and a description, and the files it will write show here.");
  assert.deepEqual(page.sfBundleRows.map((r) => r.name), ["extra", "ux"]);
  assert.deepEqual(page.sfAgentRows.map((r) => r.name), ["backend", "ux-designer"]);
});

test("typing a description updates the meter, the coach and, after a pause, the server's preview", async () => {
  const { page, timers, server } = await open("/agents/skills/new");
  type(page, "name", "figma-handoff");
  type(page, "description", "Turns a Figma frame into specs.");
  assert.equal(page.sfMeterText, "31 / 1024");
  assert.deepEqual(page.sfCoach.map((c) => c.state), ["ok", "no", "no"]);
  assert.equal(saves(server, true).length, 0, "nothing is asked while typing");
  assert.equal(page.sv.canSave, true);
  assert.equal(page.sv.footer, "2 unsaved changes · writes skills/figma-handoff/");

  type(page, "description", GOOD);
  assert.deepEqual(page.sfCoach.map((c) => c.state), ["ok", "ok", "ok"]);
  await timers.fire();
  assert.equal(saves(server, true).length, 1, "one dry run for the last keystroke");
  const sent = saves(server, true)[0][2];
  assert.equal(sent.skill.name, "figma-handoff");
  assert.equal(sent.skill.description, GOOD);
  assert.equal(sent.previous_name, undefined);
  assert.equal(page.sfHasPreview, true);
  assert.equal(saves(server, false).length, 0, "nothing is written");
});

test("Create skill saves with the readers picked, shows the toast and lands on the skill", async () => {
  const { page, server, pushed } = await open("/agents/skills/new");
  type(page, "name", "figma-handoff");
  type(page, "description", GOOD);
  page.sfToggleBundle(ev({ name: "ux" }));
  page.sfToggleAgent(ev({ name: "backend" }));
  assert.equal(page.sfReachText, "Reaches 2 agents: backend, ux-designer. Leave both empty to keep the skill on the shelf.");
  await page.saveEditor();
  assert.deepEqual(saves(server, false)[0][2].add_to, { agents: ["backend"], bundles: ["ux"] });
  assert.deepEqual(pushed, ["/agents/skills/figma-handoff"]);
  assert.equal(page.toast.hasUndo, true);
  assert.equal(page.skillEditing, false);
  assert.ok(page.catalog.skills.some((s) => s.name === "figma-handoff"));
});

test("from the agent editor, a save goes back to it with the new skill added before the #fragment", async () => {
  const { page, pushed } = await open("/agents/skills/new", { search: "?name=figma&return=%2Fagents%2Fnew%23skills" });
  assert.equal(page.sfDraft.name, "figma", "the name the editor typed");
  assert.equal(page.sv.footer, "Nothing is written until you create the skill.", "a name from the link is not an edit");
  assert.equal(page.sfBackLabel, "← Back to the agent");
  type(page, "description", GOOD);
  await page.saveEditor();
  assert.deepEqual(pushed, ["/agents/new?added=figma#skills"]);
  assert.equal(page.toast.hasUndo, true, "the toast and its Undo travel with the person");
});

test("Cancel goes back without adding anything, and never leaves the site", async () => {
  const a = await open("/agents/skills/new", { search: "?return=%2Fagents%2Fux-designer%2Fedit%23skills" });
  a.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(a.pushed, ["/agents/ux-designer/edit#skills"]);
  const b = await open("/agents/skills/new", { search: "?return=https%3A%2F%2Fevil.test%2F" });
  assert.equal(b.page.sfBack, "");
  b.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(b.pushed, ["/agents/skills"]);
});

test("going back to the agent editor opens it, and the skill form is gone", async () => {
  const { page, location } = await open("/agents/skills/new", { search: "?return=%2Fagents%2Fnew%23skills" });
  page.cancelEditor({ preventDefault() {} });
  assert.equal(location.pathname, "/agents/new");
  assert.equal(page.agentEditing, true, "the editor the person came from is open");
  assert.equal(page.skillEditing, false);
  assert.equal(page.agentDraft.previousName, "");
});

test("saving from the skill form opens the agent editor with the new skill named in the address", async () => {
  const { page, pushed } = await open("/agents/skills/new", { search: "?name=figma&return=%2Fagents%2Fnew%23skills" });
  type(page, "description", GOOD);
  await page.saveEditor();
  assert.deepEqual(pushed, ["/agents/new?added=figma#skills"]);
  assert.equal(page.agentEditing, true);
  assert.equal(page.skillEditing, false);
});

test("a name another skill has is shown at once with a link and a name to try; the save stays off", async () => {
  const { page, server } = await open("/agents/skills/new");
  type(page, "name", "tdd");
  type(page, "description", GOOD);
  assert.equal(page.fieldErrors.name, "A skill named tdd already exists.");
  assert.equal(page.sfTakenHref, "/agents/skills/tdd");
  assert.equal(page.sfSuggestion, "tdd-2");
  assert.equal(page.sfNameDescribedBy, "sf-name-hint sf-name-error");
  assert.equal(page.sfNameInvalid, true);
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Fix 1 field to create the skill.");
  page.sfUseSuggestion();
  assert.equal(page.sfDraft.name, "tdd-2");
  assert.equal(page.fieldErrors.name, "");
  assert.equal(page.sv.canSave, true);
  assert.equal(saves(server, false).length, 0);
});

test("the server's NAME_TAKEN covers a catalog that moved: same link, its suggestion", async () => {
  const { page, server, timers } = await open("/agents/skills/new");
  type(page, "name", "fresh");
  type(page, "description", GOOD);
  await server.api.post("/save_catalog_skill", { skill: { name: "fresh", description: "x", body: "", files: [], deleted_files: [] }, add_to: { agents: [], bundles: [] } });
  await timers.fire();
  assert.equal(page.fieldErrors.name, "A skill named fresh already exists.");
  assert.equal(page.sfTaken.href, "/agents/skills/fresh");
  assert.equal(page.sfSuggestion, "fresh-2");
});

test("a description over 1024 characters blocks the save, says by how much, and is linked to its field", async () => {
  const { page } = await open("/agents/skills/new");
  type(page, "name", "long");
  type(page, "description", "x".repeat(1030));
  assert.equal(page.sfMeterText, "1030 / 1024");
  assert.equal(page.sfMeterOver, "true");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.fieldErrors.description, "The description is 1030 characters, 6 over the limit of 1024. Agents read all of it, so cut it down.");
  assert.equal(page.sfDescDescribedBy, "sf-desc-hint sf-desc-coach sf-desc-error");
  type(page, "description", "x".repeat(1024));
  assert.equal(page.sfMeterOver, null);
  assert.equal(page.sv.canSave, true);
});

test("editing opens with the skill's text and readers; a rename says what it rewrites and sends previous_name", async () => {
  const { page, server, timers } = await open("/agents/skills/lint/edit");
  assert.equal(page.skillTitle, "Edit lint");
  assert.equal(page.sfDraft.previousName, "lint");
  assert.deepEqual(page.sfDraft.bundles, ["ux"]);
  assert.equal(page.saveButtonLabel, "Save skill");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sfRenameShown, false);

  type(page, "name", "linter");
  assert.equal(page.sfRenameShown, true);
  assert.equal(page.sfRenameText, "Renaming moves the folder and rewrites the 1 place in agents.json that names it. Running sessions keep the old copy until they end.");
  assert.equal(page.sv.footer, "1 unsaved change · writes skills/linter/, agents.json. Agents pick it up at their next session start.");
  await timers.fire();
  assert.equal(saves(server, true)[0][2].previous_name, "lint");
  assert.deepEqual(saves(server, true)[0][2].base_versions, { "agents.json": "sha256:a1" });
  await page.saveEditor();
  assert.equal(saves(server, false)[0][2].previous_name, "lint");
  assert.ok(page.catalog.skills.some((s) => s.name === "linter"));
});

test("an outside change blocks the save and shows it; Overwrite writes again with force", async () => {
  const { page, server, timers } = await open("/agents/skills/lint/edit");
  page.sfToggleAgent(ev({ name: "backend" }));
  await timers.fire();
  server.editOnDisk("agents.json");
  await page.saveEditor();
  assert.equal(page.sv.conflict, true);
  assert.equal(page.sv.footer, "Saving is off until you choose Reload or Overwrite.");
  assert.equal(page.sfDraft.agents.includes("backend"), true, "the edit is kept");
  await page.overwrite();
  assert.equal(saves(server, false).at(-1)[2].force, true);
  assert.equal(page.skillEditing, false);
});

test("Reload drops the edits and reads the skill again", async () => {
  const { page, server } = await open("/agents/skills/lint/edit");
  type(page, "description", "Changed.");
  server.editOnDisk("agents.json");
  await page.saveEditor();
  assert.equal(page.sv.conflict, true);
  await page.reloadEditor();
  assert.equal(page.sfDraft.description, "Lint it.");
  assert.equal(page.sv.conflict, false);
  assert.equal(page.leaveGuarded, false);
});

test("Start from fills an empty body with a template and never overwrites what was written", async () => {
  const { page } = await open("/agents/skills/new");
  page.sfStartChanged(ev({}, { value: "workflow" }));
  assert.ok(page.sfDraft.body.includes("## Steps"));
  page.sfStartChanged(ev({}, { value: "reference" }));
  assert.ok(page.sfDraft.body.includes("## Rules"), "a template gives way to another");
  page.sfDraft.body += "\nMy own rule.";
  page.sfStartChanged(ev({}, { value: "workflow" }));
  assert.ok(page.sfDraft.body.includes("My own rule."));
  assert.ok(!page.sfDraft.body.includes("## Steps"));
  assert.equal(page.sfNote, "Your instructions are not empty, so they were kept. Clear them first to start from a template.");
  assert.equal(page.sfStart, "blank");
});

test("copying another skill brings its body and files, and fills the description only when empty", async () => {
  const { page } = await open("/agents/skills/new");
  page.sfStartChanged(ev({}, { value: "copy" }));
  assert.equal(page.sfCopying, true);
  assert.deepEqual(page.sfCopyOptions.map((o) => o.name), ["lint", "tdd"]);
  await page.sfCopyChosen(ev({}, { value: "tdd" }));
  assert.equal(page.sfDraft.body, "# tdd\n");
  assert.equal(page.sfDraft.description, "Test first.");
  assert.equal(page.sfDraft.name, "", "the copy has no name yet");
});

test("extra files: a new one is named, edited and removed; an existing one removed is deleted on save", async () => {
  const { page, server } = await open("/agents/skills/lint/edit");
  page.sfNewPath = "../x";
  page.sfNewFileAdd();
  assert.equal(page.sfNewProblem, "A file stays inside the skill's folder: no “..”.");
  page.sfNewPath = "references/checklist.md";
  page.sfNewFileAdd();
  assert.deepEqual(page.sfFileRows.map((r) => [r.path, r.badge]), [["references/checklist.md", "new"]]);
  assert.equal(page.sfEditing, "references/checklist.md");
  page.sfFileInput(ev({ path: "references/checklist.md" }, { value: "- check it" }));
  assert.equal(page.sfDraft.files[0].content, "- check it");
  page.sfNewPath = "references/checklist.md";
  page.sfNewFileAdd();
  assert.equal(page.sfNewProblem, "references/checklist.md is already there.");

  await page.saveEditor();
  const sent = saves(server, false)[0][2].skill;
  assert.deepEqual(sent.files, [{ path: "references/checklist.md", content: "- check it", encoding: "utf-8" }], "the wire says utf-8");
  assert.deepEqual(sent.deleted_files, []);
});

test("an upload lands inside the skill: a folder's own name is dropped, SKILL.md is skipped, and the note says so", async () => {
  const { page } = await open("/agents/skills/new");
  type(page, "name", "up");
  const f = (name, rel, bytes) => ({ name, webkitRelativePath: rel, size: bytes.length, arrayBuffer: async () => new Uint8Array(bytes).buffer });
  const input = { hasAttribute: (a) => a === "webkitdirectory", value: "x", files: [f("a.md", "tool/references/a.md", [104, 105]), f("SKILL.md", "tool/SKILL.md", [1]), f("logo.png", "tool/logo.png", [0, 1, 2])] };
  await page.sfUpload({ currentTarget: input });
  assert.deepEqual(page.sfDraft.files.map((x) => [x.path, x.encoding, x.content]), [["references/a.md", "utf8", "hi"], ["logo.png", "base64", "AAEC"]]);
  assert.equal(page.sfUploadNote, "Added 2 files, skipped 1 (SKILL.md, hidden or outside the folder).");
  assert.equal(input.value, "", "the chooser is reset so the same folder can be chosen again");
  assert.equal(page.sfFileRows[1].binary, true);
});

test("the form is guarded against leaving with unsaved changes, and Discard goes", async () => {
  const { page } = await open("/agents/skills/new");
  const ev1 = { defaultPrevented: false, returnValue: undefined, preventDefault() { this.defaultPrevented = true; } };
  page.beforeUnload(ev1);
  assert.equal(ev1.defaultPrevented, false);
  type(page, "name", "dirty");
  page.cancelEditor({ preventDefault() {} });
  assert.equal(page.leaving !== null, true);
  assert.equal(page.skillEditing, true);
  page.keepEditing();
  assert.equal(page.sfDraft.name, "dirty");
  page.cancelEditor({ preventDefault() {} });
  page.discardAndLeave();
  assert.equal(page.skillEditing, false);
  assert.equal(page.tab, "skills");
});

test("a skill the catalog lacks has no form", async () => {
  const { page } = await open("/agents/skills/ghost/edit");
  assert.equal(page.skillEditing, true);
  assert.equal(page.sfMissing, true);
  assert.equal(page.sfReady, false);
  assert.equal(page.sv.canSave, false);
});
