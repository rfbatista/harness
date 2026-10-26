import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { describeRefusal } from "./refusals.js";
import { canSave, openSave, reduceSave, saveView, savedToast } from "./savePipeline.js";

file("agents/domain/savePipeline");

const BASE = { "agents.json": "sha256:a1" };
const writes = [{ path: "agents.json", before: "x\n", after: "y\n" }];
const refusal = (code, details = {}) => describeRefusal(new StructuredError(code, code, 409, details));

const run = (events, start = openSave(BASE)) => events.reduce(reduceSave, start);

test("an editor opens clean with the versions it read", () => {
  const s = openSave(BASE);
  assert.equal(s.phase, "clean");
  assert.deepEqual(s.baseVersions, BASE);
});

test("editing makes it dirty, and editing back to what was read makes it clean again", () => {
  assert.equal(run([{ type: "edit", dirty: true }]).phase, "dirty");
  assert.equal(run([{ type: "edit", dirty: true }, { type: "edit", dirty: false }]).phase, "clean");
});

test("a preview in flight is previewing, and its answer lands as the diff", () => {
  const s = run([{ type: "edit", dirty: true }]);
  const seq = s.seq;
  const asked = reduceSave(s, { type: "preview-start", seq });
  assert.equal(asked.phase, "previewing");
  const done = reduceSave(asked, { type: "preview-done", seq, result: { change: { id: "", summary: "Saved agents.json", writes }, versions: BASE } });
  assert.equal(done.phase, "dirty");
  assert.deepEqual(done.preview.writes, writes);
  assert.equal(done.preview.summary, "Saved agents.json");
});

test("an answer to a preview the person has since typed over is dropped", () => {
  const s = run([{ type: "edit", dirty: true }]);
  const stale = s.seq;
  const typed = reduceSave(reduceSave(s, { type: "preview-start", seq: stale }), { type: "edit", dirty: true });
  assert.equal(typed.phase, "dirty");
  const late = reduceSave(typed, { type: "preview-done", seq: stale, result: { change: { id: "", summary: "old", writes }, versions: BASE } });
  assert.equal(late.preview, null);
  assert.equal(late.phase, "dirty");
});

test("a refused save with an outside change is a conflict that blocks saving and names what changed", () => {
  const s = run([{ type: "edit", dirty: true }, { type: "save-start" }, { type: "save-refused", refusal: refusal("CATALOG_CHANGED", { current_versions: { "agents.json": "sha256:zz" }, changed: [{ path: "agents.json", theirs: "z\n" }] }) }]);
  assert.equal(s.phase, "conflict");
  assert.equal(s.saving, false);
  assert.deepEqual(s.conflict.changed, [{ path: "agents.json", theirs: "z\n" }]);
  assert.equal(canSave(s, true), false);
  assert.equal(reduceSave(s, { type: "edit", dirty: true }).phase, "conflict", "typing more does not hide it");
});

test("overwriting is the same save sent again; its success clears the conflict", () => {
  const conflict = run([{ type: "edit", dirty: true }, { type: "save-refused", refusal: refusal("CATALOG_CHANGED", { changed: [] }) }]);
  const sent = reduceSave(conflict, { type: "save-start" });
  assert.equal(sent.saving, true);
  const saved = reduceSave(sent, { type: "saved", result: { change: { id: "c1", summary: "Saved", writes }, versions: { "agents.json": "sha256:new" } } });
  assert.equal(saved.phase, "saved");
  assert.equal(saved.conflict, null);
});

test("a save keeps the new versions for the next one, and Undo's change id", () => {
  const s = run([{ type: "edit", dirty: true }, { type: "save-start" }, { type: "saved", result: { change: { id: "c1", summary: "Saved agents.json", writes }, versions: { "agents.json": "sha256:new" } } }]);
  assert.equal(s.phase, "saved");
  assert.deepEqual(s.baseVersions, { "agents.json": "sha256:new" });
  assert.deepEqual(s.saved, { id: "c1", summary: "Saved agents.json" });
  assert.equal(reduceSave(s, { type: "edit", dirty: true }).phase, "dirty");
});

test("another refusal is an error that the next edit clears", () => {
  const s = run([{ type: "edit", dirty: true }, { type: "save-refused", refusal: refusal("BUNDLE_CYCLE") }]);
  assert.equal(s.phase, "error");
  assert.equal(s.refusal.code, "BUNDLE_CYCLE");
  assert.equal(canSave(s, true), false);
  const fixed = reduceSave(s, { type: "edit", dirty: true });
  assert.equal(fixed.phase, "dirty");
  assert.equal(fixed.refusal, null);
});

test("a refused preview is shown like a refused save, and a stale refusal is dropped", () => {
  const s = run([{ type: "edit", dirty: true }]);
  const bad = reduceSave(s, { type: "preview-failed", seq: s.seq, refusal: refusal("BUNDLE_CYCLE") });
  assert.equal(bad.phase, "error");
  assert.equal(reduceSave(s, { type: "preview-failed", seq: s.seq - 1, refusal: refusal("BUNDLE_CYCLE") }).phase, "dirty");
  const changed = reduceSave(s, { type: "preview-failed", seq: s.seq, refusal: refusal("CATALOG_CHANGED", { changed: [{ path: "agents.json", theirs: null }] }) });
  assert.equal(changed.phase, "conflict");
});

test("reloading starts over from the versions just read", () => {
  const conflict = run([{ type: "edit", dirty: true }, { type: "save-refused", refusal: refusal("CATALOG_CHANGED", { changed: [] }) }]);
  const s = reduceSave(conflict, { type: "reload", baseVersions: { "agents.json": "sha256:now" } });
  assert.equal(s.phase, "clean");
  assert.deepEqual(s.baseVersions, { "agents.json": "sha256:now" });
});

test("Save is on only while there is something valid to save and nothing is in flight", () => {
  const dirty = run([{ type: "edit", dirty: true }]);
  assert.equal(canSave(dirty, true), true);
  assert.equal(canSave(dirty, false), false, "invalid");
  assert.equal(canSave(openSave(BASE), true), false, "clean");
  assert.equal(canSave(reduceSave(dirty, { type: "save-start" }), true), false, "saving");
  assert.equal(canSave(reduceSave(dirty, { type: "preview-start", seq: dirty.seq }), true), true, "previewing does not block");
});

test("the view says what the footer, the review and the banners show", () => {
  const dirty = run([{ type: "edit", dirty: true }]);
  const v = saveView(dirty, { valid: true, changes: 2, writes: ["agents.json"] });
  assert.equal(v.footer, "2 unsaved changes · writes agents.json");
  assert.equal(v.canSave, true);
  assert.equal(v.dirty, true);
  assert.equal(v.review, false);
  const done = reduceSave(reduceSave(dirty, { type: "preview-start", seq: dirty.seq }), { type: "preview-done", seq: dirty.seq, result: { change: { id: "", summary: "s", writes }, versions: BASE } });
  const w = saveView(done, { valid: true, changes: 1, writes: ["agents.json"] });
  assert.equal(w.footer, "1 unsaved change · writes agents.json");
  assert.equal(w.review, true);
  assert.equal(w.files[0].path, "agents.json");
  assert.equal(w.files[0].lines.length > 0, true);
});

test("the footer of a clean editor says nothing changed, and an invalid one says it cannot save", () => {
  assert.equal(saveView(openSave(BASE), { valid: true, changes: 0, writes: ["agents.json"] }).footer, "No changes · writes agents.json");
  const bad = saveView(run([{ type: "edit", dirty: true }]), { valid: false, changes: 1, writes: ["agents.json"] });
  assert.equal(bad.canSave, false);
});

test("a conflict view carries the diff of the disk against what the editor last previewed", () => {
  const previewed = run([
    { type: "edit", dirty: true },
    { type: "preview-done", seq: 1, result: { change: { id: "", summary: "s", writes: [{ path: "agents.json", before: "a\nb\n", after: "a\nc\n" }] }, versions: BASE } },
    { type: "save-refused", refusal: refusal("CATALOG_CHANGED", { changed: [{ path: "agents.json", theirs: "a\nb\nd\n" }] }) },
  ]);
  const v = saveView(previewed, { valid: true, changes: 1, writes: ["agents.json"] });
  assert.equal(v.conflict, true);
  assert.equal(v.canSave, false);
  assert.deepEqual(v.conflictFiles[0].lines.map((l) => l.mark + l.text), [" a", " b", "+d"]);
  assert.equal(v.conflictNote.includes("Reload"), true);
  assert.equal(v.conflictTitle, "agents.json changed on disk while you were editing.");
});

test("the toast carries the server's summary, Undo, and what running sessions keep", () => {
  const t = savedToast({ id: "c1", summary: "Saved agents.json — @ux saved" });
  assert.equal(t.text, "Saved agents.json — @ux saved");
  assert.equal(t.undoId, "c1");
  assert.equal(t.tone, "");
  assert.equal(t.note, "Changes reach sessions from their next start. Running sessions keep what they started with.");
});
