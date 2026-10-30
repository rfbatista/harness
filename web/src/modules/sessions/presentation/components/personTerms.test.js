import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryPersonTerminals } from "../../infrastructure/memory-person-terminals.js";
import { preferences } from "../../../../shared/infrastructure/storage.js";
import { DOCK_MIN_PX, RESTART_NOTE, SESSION_ENDED_NOTE } from "../../domain/personTerms.js";
import { personTerms } from "./personTerms.js";

file("sessions/presentation/personTerms");

function memoryStorage() {
  const m = new Map();
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) };
}

async function setup({ existing = [], tabHeight = 800, storage = memoryStorage() } = {}) {
  const mem = memoryPersonTerminals();
  for (const e of existing) await mem.gateway.open("s1", e);
  const prefs = preferences(storage);
  const m = mount(() => personTerms({ persons: mem.gateway, prefs, measure: () => ({ height: tabHeight }) })("s1"));
  m.instance.init();
  await flush();
  return { ...m, mem, prefs, storage };
}

const open = (root, dir) => ({ root, dir, cols: 80, rows: 24 });

test("it lists the session's terminals on load and selects the first, as a reload does", async () => {
  const { instance } = await setup({ existing: [open("worktree", "web/src"), open("resources", "")] });
  assert.deepEqual(instance.terms.map((t) => [t.root, t.label]), [["worktree", "web/src"], ["resources", "Resources"]]);
  assert.equal(instance.current.id, instance.terms[0].id);
  assert.equal(instance.count, 2);
  assert.equal(instance.visible, true);
});

test("with no terminal the dock is not there", async () => {
  const { instance } = await setup();
  assert.equal(instance.visible, false);
  assert.equal(instance.count, 0);
});

test("Open terminal here opens on the root and folder it names, and selects the new tab", async () => {
  const { instance, mem } = await setup();
  await instance.open("worktree", "web/src");
  assert.deepEqual(mem.opened[0], { sessionId: "s1", root: "worktree", dir: "web/src", cols: 80, rows: 24 });
  assert.equal(instance.terms.length, 1);
  assert.equal(instance.current.label, "web/src");
  assert.equal(instance.refusal, null);
});

test("the open request the tree's action sends is the same open", async () => {
  const { instance, mem } = await setup();
  await instance.openRequested({ detail: { root: "resources", dir: "" } });
  assert.equal(mem.opened[0].root, "resources");
  assert.equal(instance.terms.length, 1);
});

test("an open request for another session is not this dock's", async () => {
  const { instance, mem } = await setup();
  await instance.openRequested({ detail: { sessionId: "s2", root: "worktree", dir: "" } });
  assert.equal(mem.opened.length, 0);
});

test("a second request while one is starting waits for it", async () => {
  const { instance, mem } = await setup();
  const first = instance.open("worktree", "");
  assert.equal(instance.opening, true);
  await instance.open("worktree", "web");
  await first;
  assert.equal(mem.opened.length, 1);
  assert.equal(instance.opening, false);
});

test("a refused open shows why in place of the terminal, and Dismiss takes it away", async () => {
  const { instance, mem } = await setup();
  mem.refuseNext("TERMINAL_SERVER_EXPOSED", "The harness listens beyond this machine.");
  await instance.open("worktree", "");
  assert.equal(instance.terms.length, 0);
  assert.equal(instance.refusal.code, "TERMINAL_SERVER_EXPOSED");
  assert.ok(instance.refusal.title.startsWith("Terminals are off"));
  assert.equal(instance.visible, true, "the refusal has room to show");
  instance.dismissRefusal();
  assert.equal(instance.refusal, null);
  assert.equal(instance.visible, false);
});

test("a network failure is shown as a refusal with the server's words", async () => {
  const { instance, mem } = await setup();
  mem.gateway.open = async () => { throw new Error("boom"); };
  await instance.open("worktree", "");
  assert.equal(instance.refusal.title, "Cannot open a terminal here.");
});

test("closing a tab hangs up its shell, and the next tab is selected", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", ""), open("worktree", "web"), open("worktree", "docs")] });
  const [a, b, c] = instance.terms.map((t) => t.id);
  instance.select(b);
  await instance.close(b);
  assert.deepEqual(mem.closed, [b]);
  assert.deepEqual(instance.terms.map((t) => t.id), [a, c]);
  assert.equal(instance.current.id, c);
  await instance.close(c);
  assert.equal(instance.current.id, a);
  await instance.close(a);
  assert.equal(instance.current, null);
  assert.equal(instance.visible, false);
});

test("a terminal the server ended keeps its tab, with its note", async () => {
  const { instance, mem } = await setup({ existing: [open("resources", ""), open("worktree", "")] });
  const [res, wt] = instance.terms.map((t) => t.id);
  mem.end(res, "root_gone", SESSION_ENDED_NOTE);
  assert.equal(instance.terms.length, 2);
  const ended = instance.terms.find((t) => t.id === res);
  assert.equal(ended.ended.strong, SESSION_ENDED_NOTE);
  assert.equal(ended.ended.reopen, false);
  assert.equal(instance.count, 1, "the badge counts the terminals that run");
  assert.equal(instance.terms.find((t) => t.id === wt).ended, null);
});

test("an ended terminal's last screen stays, and a new one opens on the same folder in the same slot", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", "web/src"), open("worktree", "")] });
  const [first, second] = instance.terms.map((t) => t.id);
  mem.end(first, "exited", "The shell exited.");
  await instance.reopen(first);
  assert.equal(mem.opened.at(-1).dir, "web/src");
  assert.equal(instance.terms.length, 2);
  assert.ok(instance.terms[0].id !== first, "the same slot holds the new terminal");
  assert.equal(instance.terms[1].id, second);
  assert.equal(instance.current.id, instance.terms[0].id);
});

test("after a harness restart the pane the page still holds says so, and offers a new one", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", "web/src")] });
  const id = instance.terms[0].id;
  mem.restart();
  await instance.load();
  const t = instance.terms.find((x) => x.id === id);
  assert.equal(t.ended.strong, RESTART_NOTE);
  assert.equal(t.ended.reopen, true);
  assert.equal(instance.count, 0);
});

test("when the session ended, a resources pane that is gone says the session ended, not that the harness restarted", async () => {
  const { instance, mem } = await setup({ existing: [open("resources", ""), open("worktree", "")] });
  instance.setSessionEnded(true);
  const res = instance.terms[0];
  assert.equal(res.ended.strong, SESSION_ENDED_NOTE);
  assert.equal(instance.terms[1].ended, null, "the worktree terminal keeps running");
  mem.restart();
  await instance.load();
  assert.equal(instance.terms[0].ended.strong, SESSION_ENDED_NOTE, "the note it has stays");
  assert.equal(instance.terms[1].ended.strong, RESTART_NOTE);
});

test("it reloads the list when the feed says events were missed", async () => {
  const { instance, mem } = await setup();
  await mem.gateway.open("s1", open("worktree", "web"));
  instance.feedStatus("resynced");
  await flush();
  assert.equal(instance.terms.length, 1);
});

test("a read-only resources terminal says so", async () => {
  const { instance } = await setup({ existing: [open("resources", "")] });
  assert.equal(instance.current.readonly, true);
  assert.equal(instance.terms[0].readonly, true);
});

test("it tells the page how many terminals run, for the Files tab's badge", async () => {
  const { instance, dispatched, mem } = await setup({ existing: [open("worktree", "")] });
  assert.deepEqual(dispatched.at(-1), { name: "person-terminals-count", detail: { sessionId: "s1", count: 1 } });
  await instance.open("worktree", "web");
  assert.equal(dispatched.at(-1).detail.count, 2);
  mem.end(instance.terms[0].id, "exited", "The shell exited.");
  assert.equal(dispatched.at(-1).detail.count, 1);
});

test("the dock keeps its height across a reload, within the tab", async () => {
  const storage = memoryStorage();
  const first = await setup({ storage });
  first.instance.setHeight(300);
  assert.equal(first.instance.height, 300);
  const second = await setup({ storage });
  assert.equal(second.instance.height, 300);
  assert.equal(second.instance.dockStyle, "--dock: 300px");

  const small = await setup({ storage, tabHeight: 200 });
  assert.equal(small.instance.dockStyle, "--dock: 200px", "a shorter tab limits it");
});

test("a height outside the limits is clamped", async () => {
  const { instance } = await setup();
  instance.setHeight(5);
  assert.equal(instance.height, DOCK_MIN_PX);
  instance.setHeight(9000);
  assert.equal(instance.height, 800);
});

test("the handle moves by key, reports its value, and is a separator", async () => {
  const { instance } = await setup();
  instance.setHeight(400);
  const prevented = [];
  instance.handleKey({ key: "ArrowUp", preventDefault: () => prevented.push(1) });
  assert.equal(instance.height, 440);
  instance.handleKey({ key: "ArrowDown", preventDefault: () => prevented.push(1) });
  instance.handleKey({ key: "ArrowDown", preventDefault: () => prevented.push(1) });
  assert.equal(instance.height, 360);
  assert.equal(prevented.length, 3);
  instance.handleKey({ key: "a", preventDefault: () => prevented.push(1) });
  assert.equal(prevented.length, 3, "another key is left alone");
  assert.equal(instance.handleValue, Math.round((360 / 800) * 100));
});

test("dragging the top edge sets the height from the pointer, and the drag ends on release", async () => {
  const { instance } = await setup();
  const handle = { setPointerCapture() {}, releasePointerCapture() {} };
  instance.dragStart({ pointerId: 1, currentTarget: handle, clientY: 500 });
  assert.equal(instance.dragging, true);
  instance.dragMove({ clientY: 380 }, { top: 0, bottom: 800 });
  assert.equal(instance.height, 420, "the dock reaches from the pointer to the bottom of the tab");
  instance.dragEnd({ pointerId: 1, currentTarget: handle });
  assert.equal(instance.dragging, false);
  instance.dragMove({ clientY: 100 }, { top: 0, bottom: 800 });
  assert.equal(instance.height, 420, "a pointer that is not dragging changes nothing");
});

test("expand fills the tab, and one click or Escape restores it, and the choice is remembered", async () => {
  const storage = memoryStorage();
  const { instance } = await setup({ storage, existing: [open("worktree", "")] });
  instance.toggleExpanded();
  assert.equal(instance.expanded, true);
  assert.equal(instance.expandLabel, "Restore terminals");
  const again = await setup({ storage, existing: [open("worktree", "")] });
  assert.equal(again.instance.expanded, true);

  instance.escape({ key: "Escape" });
  assert.equal(instance.expanded, false);
  instance.escape({ key: "Escape" });
  assert.equal(instance.expanded, false, "Escape does nothing when the dock is not expanded");
});

test("expanding and restoring tell the Files pane, so it can hide the tree and the viewer", async () => {
  const { instance, dispatched } = await setup();
  instance.toggleExpanded();
  assert.deepEqual(dispatched.at(-1), { name: "person-terminals-expanded", detail: { sessionId: "s1", expanded: true } });
  instance.toggleExpanded();
  assert.equal(dispatched.at(-1).detail.expanded, false);
});

test("the tab row has one entry per terminal, the one on show current, with a roving tab stop", async () => {
  const { instance } = await setup({ existing: [open("worktree", "web/src"), open("resources", "")] });
  const [a, b] = instance.tabs;
  assert.equal(a.isCurrent, true);
  assert.equal(a.selected, "true");
  assert.equal(a.tabindex, "0");
  assert.equal(b.tabindex, "-1");
  assert.equal(b.locked, true);
  assert.equal(b.where, "", "a root's own terminal is labelled by the root alone");
  assert.equal(b.label, "Resources");
  assert.equal(a.closeLabel, "Close terminal: web/src");
  assert.equal(b.readonlyNote, true);
  instance.select(b.id);
  assert.equal(instance.tabs[1].currentAttr, "");
  assert.equal(instance.tabs[0].currentAttr, null);
});

test("the arrow keys walk the tabs and wrap", async () => {
  const { instance } = await setup({ existing: [open("worktree", ""), open("worktree", "web"), open("worktree", "docs")] });
  const press = (key) => instance.tabKey({ key, preventDefault() {} });
  press("ArrowRight");
  assert.equal(instance.current.label, "web");
  press("End");
  assert.equal(instance.current.label, "docs");
  press("ArrowRight");
  assert.equal(instance.current.label, "Worktree", "past the last is the first");
  press("ArrowLeft");
  assert.equal(instance.current.label, "docs");
});

test("a dropped connection offers Reconnect for the terminal on show, and a terminal's status stays inside the dock", async () => {
  const { instance, dispatched } = await setup({ existing: [open("worktree", "")] });
  const id = instance.terms[0].id;
  assert.equal(instance.down, "");
  instance.terminalStatus({ detail: { id, state: "offline" } });
  assert.equal(instance.down, id);
  instance.reconnect();
  assert.deepEqual(dispatched.at(-1), { name: "terminal-reconnect-requested", detail: { id } });
  instance.terminalStatus({ detail: { id, state: "live" } });
  assert.equal(instance.down, "");
});

test("a shell that exits shows its note even when the server's event is missed, and the event's sentence wins when it comes", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", ""), open("resources", "")] });
  const [wt, res] = instance.terms.map((t) => t.id);
  instance.terminalExited({ detail: { id: wt, code: 0 } });
  assert.equal(instance.terms[0].ended.strong, "The shell exited.");
  assert.equal(instance.terms[0].ended.reopen, true);
  instance.terminalExited({ detail: { id: res, code: 2 } });
  assert.equal(instance.terms[1].ended.strong, "The shell exited with code 2.");
  mem.end(wt, "exited", "The shell exited.");
  assert.equal(instance.terms[0].ended.strong, "The shell exited.");
});

test("a resources shell that exits after the session ended says the session ended", async () => {
  const { instance } = await setup({ existing: [open("resources", "")] });
  instance.sessionEnded = true;
  instance.terminalExited({ detail: { id: instance.terms[0].id, code: 0 } });
  assert.equal(instance.terms[0].ended.strong, SESSION_ENDED_NOTE);
});

test("while the tab is hidden and has no height, the person's height is kept as it is, not squeezed to nothing", async () => {
  const storage = memoryStorage();
  const first = await setup({ storage });
  first.instance.setHeight(400);
  const hidden = await setup({ storage, tabHeight: 0 });
  assert.equal(hidden.instance.height, 400);
  assert.equal(hidden.instance.dockStyle, "--dock: 400px");
  assert.equal(hidden.instance.handleValue, 0);
});

test("Escape typed in a terminal is the shell's: it does not restore the dock", async () => {
  const { instance } = await setup();
  instance.toggleExpanded();
  const screen = document.createElement("div");
  screen.className = "screen";
  const inside = document.createElement("textarea");
  screen.append(inside);
  instance.escape({ key: "Escape", target: inside });
  assert.equal(instance.expanded, true);
  instance.escape({ key: "Escape", target: document.createElement("button") });
  assert.equal(instance.expanded, false);
});

test("a terminal on a folder is labelled by its root and the folder; on the root, by the root alone", async () => {
  const { instance } = await setup({ existing: [open("worktree", "web/src"), open("worktree", ""), open("resources", "docs")] });
  const [folder, root, res] = instance.tabs;
  assert.deepEqual([folder.where, folder.label], ["Worktree", "web/src"]);
  assert.deepEqual([root.where, root.label], ["", "Worktree"]);
  assert.deepEqual([res.where, res.label], ["Resources", "docs"]);
});

test("closing the last terminal of an expanded dock restores the dock: nothing stays hidden behind it", async () => {
  const storage = memoryStorage();
  const { instance, dispatched, prefs } = await setup({ storage, existing: [open("worktree", "")] });
  instance.toggleExpanded();
  assert.equal(instance.expandedAttr, "");
  await instance.close(instance.terms[0].id);
  assert.equal(instance.expanded, false);
  assert.equal(instance.expandedAttr, null);
  assert.equal(prefs.get("person-terms.expanded"), null, "the stored flag is cleared");
  assert.deepEqual(dispatched.at(-1), { name: "person-terminals-expanded", detail: { sessionId: "s1", expanded: false } });
});

test("a terminal that ends does not collapse the dock while its tab stays", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", "")] });
  instance.toggleExpanded();
  mem.end(instance.terms[0].id, "exited", "The shell exited.");
  assert.equal(instance.expanded, true, "the ended tab is still there to show its note");
});

test("a page loaded with no terminal ignores a stored expanded flag", async () => {
  const storage = memoryStorage();
  const first = await setup({ storage, existing: [open("worktree", "")] });
  first.instance.toggleExpanded();
  const later = await setup({ storage });
  assert.equal(later.instance.expanded, false);
  assert.equal(later.instance.expandedAttr, null);
  assert.equal(later.prefs.get("person-terms.expanded"), null);
  assert.ok(!later.dispatched.some((d) => d.name === "person-terminals-expanded" && d.detail.expanded), "nothing told the page the dock is expanded");
});

test("a refusal keeps an expanded dock, since it has something to show", async () => {
  const { instance, mem } = await setup({ existing: [open("worktree", "")] });
  instance.toggleExpanded();
  mem.refuseNext("TERMINAL_HOST_REFUSED", "no");
  await instance.close(instance.terms[0].id);
  assert.equal(instance.expanded, false);
  await instance.open("worktree", "");
  assert.equal(instance.expandedAttr, null);
});
