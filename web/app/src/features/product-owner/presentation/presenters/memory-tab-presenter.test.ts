import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProjectMemory } from "../../application/project-memory";
import { ProductOwnerError } from "../../application/ports";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { entry } from "../../testing/fixtures";
import { MemoryTabPresenter, type EntryRow } from "./memory-tab-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const at = (day: number, hour = 9, minute = 20) => new Date(2026, 10, day, hour, minute);

const setup = async (entries = [entry({ entryId: "a", text: "Alpha", savedAt: at(20, 10, 0), threadId: "t1", threadTitle: "What blocks the release" }), entry({ entryId: "b", text: "Beta decision", savedAt: at(22, 11, 32), threadId: "t2", threadTitle: "Shaping the digest", changedAt: at(23, 8, 5), changedBy: "person" })]) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedMemory("p1", entries);
  const memory = new ProjectMemory({ gateway: server, events: server, clock: () => at(24, 14, 21) });
  const presenter = new MemoryTabPresenter(memory, "p1");
  memory.attach("p1");
  await settled();
  return { server, memory, presenter };
};
const entries = (p: MemoryTabPresenter): EntryRow[] => p.viewModel.rows.filter((r): r is EntryRow => r.kind === "entry");

describe("the Memory tab", () => {
  test("lists the entries newest first with the date, the thread link and who edited", async () => {
    const { presenter } = await setup();
    const vm = presenter.viewModel;
    assert.equal(vm.count, 2);
    assert.match(vm.sentence, /^2 entries\. The product owner reads all of them at the start of every thread\./);
    const [b, a] = entries(presenter);
    assert.equal(b!.entryId, "b");
    assert.equal(b!.saved, "Saved Sun 22 Nov, 11:32");
    assert.equal(b!.edited, "edited by you Mon 23 Nov");
    assert.deepEqual({ href: b!.threadHref, title: b!.threadTitle, threadId: b!.threadId }, { href: "/projects/p1/product-owner/threads/t2", title: "Shaping the digest", threadId: "t2" });
    assert.equal(a!.edited, "");
    assert.equal(a!.saved, "Saved Fri 20 Nov, 10:00");
  });

  test("says when the product owner changed an entry, and names no thread it does not know", async () => {
    const { presenter } = await setup([entry({ entryId: "x", changedAt: at(23), changedBy: "product_owner", threadId: "", threadTitle: "" })]);
    const row = entries(presenter)[0]!;
    assert.equal(row.edited, "edited by the product owner Mon 23 Nov");
    assert.equal(row.threadHref, null);
  });

  test("shows the empty state of a project with nothing remembered", async () => {
    const { presenter } = await setup([]);
    assert.equal(presenter.viewModel.empty, true);
    assert.equal(presenter.viewModel.rows.length, 0);
    assert.equal(presenter.viewModel.sentence.startsWith("0 entries"), true);
  });

  test("is loading until the first read, and shows a failed read with a retry", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    const memory = new ProjectMemory({ gateway: server, events: server });
    const presenter = new MemoryTabPresenter(memory, "p1");
    memory.attach("p1");
    assert.equal(presenter.viewModel.loading, true);
    await settled();
    assert.equal(presenter.viewModel.loading, false);
    assert.equal(presenter.viewModel.error, "offline");
    assert.equal(presenter.viewModel.empty, false);
    server.failWith(null);
    presenter.retry();
    await settled();
    assert.equal(presenter.viewModel.error, "");
    assert.equal(presenter.viewModel.empty, true);
  });

  test("an entry that arrives live is fresh", async () => {
    const { server, presenter } = await setup();
    assert.equal(entries(presenter).some((r) => r.fresh), false);
    server.remember("p1", entry({ entryId: "c", text: "Gamma", savedAt: at(24, 9, 0) }), "saved");
    await settled();
    assert.deepEqual(entries(presenter).map((r) => [r.entryId, r.fresh]), [["c", true], ["b", false], ["a", false]]);
  });

  test("the filter narrows by text, and says when nothing fits", async () => {
    const { presenter } = await setup();
    presenter.setFilter("BETA");
    assert.deepEqual(entries(presenter).map((r) => r.entryId), ["b"]);
    assert.equal(presenter.viewModel.noMatch, false);
    assert.equal(presenter.viewModel.count, 2);
    presenter.setFilter("zzz");
    assert.equal(presenter.viewModel.noMatch, true);
    assert.equal(presenter.viewModel.empty, false);
    presenter.setFilter("");
    assert.equal(entries(presenter).length, 2);
  });

  const row = (p: MemoryTabPresenter, id: string): EntryRow => entries(p).find((r) => r.entryId === id)!;

  test("edits an entry where it stands: Edit opens a draft with the text, Save sends it and shows the answer", async () => {
    const { server, presenter } = await setup();
    presenter.startEdit("a");
    assert.equal(row(presenter, "a").mode, "edit");
    assert.deepEqual({ ...row(presenter, "a").edit! }, { draft: "Alpha", length: 5, limit: 4000, showCount: false, over: false, canSave: false, saving: false, error: "" });
    presenter.setDraft("Alpha, edited");
    assert.equal(row(presenter, "a").edit!.canSave, true);
    await presenter.save();
    assert.equal(row(presenter, "a").mode, "view");
    assert.equal(row(presenter, "a").text, "Alpha, edited");
    assert.match(row(presenter, "a").edited, /^edited by you /);
    assert.equal((await server.memory("p1")).find((e) => e.entryId === "a")!.text, "Alpha, edited");
  });

  test("Cancel leaves the entry as it was, and one entry is in edit at a time", async () => {
    const { presenter } = await setup();
    presenter.startEdit("a");
    presenter.setDraft("scratch");
    presenter.startEdit("b");
    assert.deepEqual([row(presenter, "a").mode, row(presenter, "b").mode], ["view", "edit"]);
    assert.equal(row(presenter, "a").text, "Alpha");
    presenter.cancel();
    assert.equal(row(presenter, "b").mode, "view");
  });

  test("counts code points against 4,000 and blocks an empty or too long text", async () => {
    const { presenter } = await setup();
    presenter.startEdit("a");
    presenter.setDraft("😀".repeat(4000));
    assert.deepEqual([row(presenter, "a").edit!.length, row(presenter, "a").edit!.over, row(presenter, "a").edit!.canSave, row(presenter, "a").edit!.showCount], [4000, false, true, true]);
    presenter.setDraft("😀".repeat(4001));
    assert.deepEqual([row(presenter, "a").edit!.length, row(presenter, "a").edit!.over, row(presenter, "a").edit!.canSave], [4001, true, false]);
    presenter.setDraft("   ");
    assert.equal(row(presenter, "a").edit!.canSave, false);
    presenter.setDraft("x".repeat(3500));
    assert.equal(row(presenter, "a").edit!.showCount, true);
    presenter.setDraft("x".repeat(3499));
    assert.equal(row(presenter, "a").edit!.showCount, false);
  });

  test("a refusal keeps the typed text and shows the refusal", async () => {
    const { server, presenter } = await setup();
    presenter.startEdit("a");
    presenter.setDraft("Keep this text");
    server.failWith(new ProductOwnerError("INVALID_INPUT", "the entry is empty or too long"));
    await presenter.save();
    assert.equal(row(presenter, "a").mode, "edit");
    assert.equal(row(presenter, "a").edit!.draft, "Keep this text");
    assert.equal(row(presenter, "a").edit!.error, "the entry is empty or too long");
    assert.equal(row(presenter, "a").edit!.saving, false);
  });

  test("an entry the server no longer has leaves the list and closes its edit", async () => {
    const { server, presenter } = await setup();
    presenter.startEdit("a");
    presenter.setDraft("Late");
    server.seedMemory("p1", [entry({ entryId: "b", text: "Beta decision", savedAt: at(22, 11, 32) })]);
    await presenter.save();
    assert.equal(entries(presenter).some((r) => r.entryId === "a"), false);
    assert.equal(presenter.viewModel.error, "");
  });

  test("a live change to the entry being edited keeps the typed text", async () => {
    const { server, presenter } = await setup();
    presenter.startEdit("a");
    presenter.setDraft("My draft");
    server.remember("p1", entry({ entryId: "a", text: "Alpha by the product owner", savedAt: at(20, 10, 0), changedAt: at(24), changedBy: "product_owner" }), "changed");
    await settled();
    assert.equal(row(presenter, "a").text, "Alpha by the product owner");
    assert.equal(row(presenter, "a").edit!.draft, "My draft");
  });

  test("the entry being edited that is dropped closes its edit", async () => {
    const { server, presenter } = await setup();
    presenter.startEdit("a");
    server.forget("a");
    await settled();
    assert.equal(entries(presenter).some((r) => r.entryId === "a"), false);
    presenter.startEdit("b");
    assert.equal(row(presenter, "b").mode, "edit");
  });

  test("Delete opens a danger confirm; Keep it changes nothing", async () => {
    const { server, presenter } = await setup();
    presenter.askDelete("a");
    assert.equal(row(presenter, "a").mode, "delete");
    presenter.keep();
    assert.equal(row(presenter, "a").mode, "view");
    assert.equal((await server.memory("p1")).length, 2);
  });

  test("Delete entry sends the delete and a one-line receipt stands where the entry was", async () => {
    const { server, presenter } = await setup();
    presenter.askDelete("b");
    await presenter.confirmDelete();
    assert.deepEqual(presenter.viewModel.rows.map((r) => r.kind), ["receipt", "entry"]);
    assert.equal(presenter.viewModel.rows[0]!.kind === "receipt" && presenter.viewModel.rows[0]!.text, 'Deleted "Beta decision" · 14:21');
    assert.equal(presenter.viewModel.count, 1);
    assert.equal((await server.memory("p1")).length, 1);
  });

  test("a failed delete keeps the entry and says why", async () => {
    const { server, presenter } = await setup();
    presenter.askDelete("a");
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    await presenter.confirmDelete();
    assert.equal(row(presenter, "a").mode, "delete");
    assert.equal(row(presenter, "a").deleteError, "offline");
  });

  test("asks the view to move focus: into the text on Edit, back to the button on Cancel and Save, to the receipt on delete", async () => {
    const { presenter } = await setup();
    const focus = () => ({ ...presenter.viewModel.focus });
    presenter.startEdit("a");
    assert.deepEqual([focus().entryId, focus().target], ["a", "text"]);
    const first = focus().seq;
    presenter.cancel();
    assert.deepEqual([focus().entryId, focus().target], ["a", "edit-button"]);
    assert.ok(focus().seq > first);
    presenter.startEdit("a");
    presenter.setDraft("Changed");
    await presenter.save();
    assert.deepEqual([focus().entryId, focus().target], ["a", "edit-button"]);
    presenter.askDelete("b");
    assert.deepEqual([focus().entryId, focus().target], ["b", "delete-confirm"]);
    presenter.keep();
    assert.deepEqual([focus().entryId, focus().target], ["b", "delete-button"]);
    presenter.askDelete("b");
    await presenter.confirmDelete();
    assert.deepEqual([focus().entryId, focus().target], ["b", "receipt"]);
  });

  test("brings an entry into view when the page was opened at it", async () => {
    const { server, memory } = await setup();
    const presenter = new MemoryTabPresenter(memory, "p1", "a");
    assert.deepEqual([presenter.viewModel.focus.entryId, presenter.viewModel.focus.target], ["a", "entry"]);
    assert.equal(server.followers, 1);
  });

  test("a note asks for an entry, and once the view moved focus a later visit does not move it again", async () => {
    const { presenter } = await setup();
    presenter.showEntry("b");
    assert.deepEqual([presenter.viewModel.focus.entryId, presenter.viewModel.focus.target], ["b", "entry"]);
    const seq = presenter.viewModel.focus.seq;
    presenter.focused();
    assert.equal(presenter.viewModel.focus.target, "");
    assert.equal(presenter.viewModel.focus.seq, seq);
  });
});
