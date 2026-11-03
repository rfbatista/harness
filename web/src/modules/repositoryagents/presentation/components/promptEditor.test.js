import { flush } from "../../../../shared/testing/doubles.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRepositoryAgents } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { repositoryAgentsTab } from "./repositoryAgentsTab.js";

file("repositoryagents/presentation/components/promptEditor");

async function open() {
  const memory = memoryRepositoryAgents(makeWorld());
  const el = document.createElement("section");
  Object.assign(el.dataset, { projectId: "p1", repositoryId: "r1" });
  const m = mount(repositoryAgentsTab({ gateway: memory.gateway }), { el });
  await m.instance.init();
  return { ...m, ed: m.instance, gateway: memory.gateway };
}

test("Edit prompt opens the editor on the agent with its text; the list marks the agents that have one", async () => {
  const { ed } = await open();
  await ed.openPrompt("reviewer");
  assert.equal(ed.editorOpen, true);
  assert.equal(ed.editorTitle, "Repository prompt · reviewer in atlas-api");
  assert.equal(ed.draft, "Run make check before every commit.\nThen open the PR.");
  assert.equal(ed.dirty, false);
  assert.deepEqual(ed.editorAgents.filter((a) => a.mark === "set").map((a) => a.name), ["reviewer", "backend"]);
  assert.ok(ed.editorIntro.includes("never replace it"));
  assert.ok(ed.editorIntro.includes("another agent spawns it"));
});

test("Write then Save stores the prompt; the tab row and the list mark follow", async () => {
  const { ed, gateway } = await open();
  await ed.openPrompt("tester");
  ed.draft = "Cover the edge cases.";
  assert.equal(ed.dirty, true);
  assert.equal(ed.barText, "Unsaved changes");
  await ed.savePrompt();
  assert.equal(ed.dirty, false);
  assert.equal((await gateway.getRepositoryPrompt("r1", "tester")).content, "Cover the edge cases.");
  assert.equal(ed.assignedRows.find((r) => r.name === "tester").summary, "“Cover the edge cases.”");
  assert.equal(ed.editorAgents.find((a) => a.name === "tester").mark, "set");
});

test("a blank save clears the prompt, and its row says there is none", async () => {
  const { ed } = await open();
  await ed.openPrompt("reviewer");
  ed.draft = "  \n";
  await ed.savePrompt();
  assert.equal(ed.draft, "");
  assert.equal(ed.assignedRows.find((r) => r.name === "reviewer").summary, "No repository prompt");
});

test("Preview shows the server's text: the agent's own prompt, then the repository section; blank adds nothing", async () => {
  const { ed } = await open();
  await ed.openPrompt("tester");
  ed.draft = "Draft only.";
  await ed.setEditorView("preview");
  assert.equal(ed.previewView.own.text, "You write tests.");
  assert.equal(ed.previewView.section, "## Repository prompt: atlas-api\n\nDraft only.");
  assert.equal(ed.previewView.nothing, "");
  assert.ok(ed.previewView.intro.includes("A session that is already running keeps what it started with."));
  ed.draft = "";
  await ed.setEditorView("preview");
  assert.equal(ed.previewView.nothing, "Nothing is added. The agent gets only its own prompt.");
});

test("a stale save keeps the draft, shows both versions on request, and re-applies the draft over the new one", async () => {
  const { ed, gateway } = await open();
  await ed.openPrompt("reviewer");
  const mine = ed.prompt;
  await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: "Theirs.", version: mine.version });
  ed.draft = "Mine.";
  await ed.savePrompt();
  assert.equal(ed.draft, "Mine.", "the draft is kept");
  assert.equal(ed.conflict.content, "Theirs.");
  assert.equal(ed.canSave, false, "Save is off until the person chooses");
  assert.ok(ed.barText.startsWith("Not saved: you edited version"));
  ed.showTheirVersion();
  assert.equal(ed.showTheirs, true);
  ed.reapplyMine();
  assert.equal(ed.conflict, null);
  assert.equal(ed.draft, "Mine.");
  assert.equal(ed.canSave, true);
  await ed.savePrompt();
  assert.equal((await gateway.getRepositoryPrompt("r1", "reviewer")).content, "Mine.");
});

test("another person's save reaches a clean editor silently, and a dirty one as a conflict", async () => {
  const { ed, gateway } = await open();
  await ed.openPrompt("reviewer");
  let p = await gateway.getRepositoryPrompt("r1", "reviewer");
  p = await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: "One.", version: p.version });
  await flush(); await flush(); await flush();
  assert.equal(ed.draft, "One.");
  assert.equal(ed.conflict, null);
  ed.draft = "Mine.";
  await gateway.saveRepositoryPrompt({ repositoryId: "r1", agent: "reviewer", content: "Two.", version: p.version });
  await flush(); await flush(); await flush();
  assert.equal(ed.draft, "Mine.");
  assert.equal(ed.conflict.content, "Two.");
});

test("over the limit disables Save, and the counter says so in words", async () => {
  const { ed } = await open();
  await ed.openPrompt("tester");
  ed.draft = "x".repeat(8012);
  assert.equal(ed.count.over, true);
  assert.equal(ed.count.text, "12 over the limit of 8,000 characters");
  assert.equal(ed.canSave, false);
  ed.draft = "x".repeat(8000);
  assert.equal(ed.canSave, true);
  assert.equal(ed.count.text, "8,000 of 8,000 characters · plain text");
});

test("an unassigned agent's editor says the prompt is kept but not used, and offers Assign again", async () => {
  const { ed } = await open();
  await ed.openPrompt("backend");
  assert.ok(ed.notUsed.text.includes("This prompt is kept but not used."));
  assert.equal(ed.notUsed.canAssign, true);
  await ed.openPrompt("docs");
  assert.equal(ed.notUsed.canAssign, false);
  await ed.openPrompt("tester");
  assert.equal(ed.notUsed, null);
});

test("a chip inserts one example line; Discard drops the draft; unsaved text is not left behind silently", async () => {
  const { ed } = await open();
  await ed.openPrompt("tester");
  ed.insertChip("test");
  assert.equal(ed.draft, "Run make check before every commit; never skip the race tests.");
  ed.draft = "First line";
  ed.insertChip("commit");
  assert.equal(ed.draft, "First line\nWrite commit messages in Portuguese.");
  await ed.openPrompt("reviewer");
  assert.equal(ed.editorAgent, "tester", "refused while there are unsaved changes");
  assert.ok(ed.keepNote.includes("Save or discard"));
  ed.closeEditor();
  assert.equal(ed.editorOpen, true);
  ed.discard();
  assert.equal(ed.dirty, false);
  await ed.openPrompt("reviewer");
  assert.equal(ed.editorAgent, "reviewer");
  ed.closeEditor();
  assert.equal(ed.editorOpen, false);
});
