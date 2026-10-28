import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTokenSavers } from "../../../tokensavers/infrastructure/memory-gateway.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { taskEditor } from "./taskEditor.js";

file("tasks/presentation/taskEditor");

function setup({ task = makeTask(), sessions = [], store } = {}) {
  const memory = memoryTasks({ projects: ["p1"], tasks: [task], sessions });
  const visited = [];
  let reloads = 0;
  const el = seededElement(taskDTO(task));
  const mounted = mount(taskEditor({ gateway: memory.gateway, navigate: (u) => visited.push(u), reload: () => reloads++, store }), { el });
  mounted.instance.init();
  return { ...mounted, memory, visited, reloads: () => reloads };
}

test("a status change is saved status-only and the page reloads so the rail regroups", async () => {
  const { instance, memory, reloads } = setup();
  assert.equal(instance.status, "in_progress");
  instance.status = "review";
  await instance.changeStatus();
  assert.equal(memory.tasks()[0].status, "review");
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", status: "review" } });
  assert.equal(reloads(), 1);
});

test("editing the text does not resend the status", async () => {
  const { instance, memory } = setup();
  instance.startEditTask();
  instance.draftTitle = "Add the SSE feed";
  await instance.save();
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", title: "Add the SSE feed", description: "Stream session changes to clients." } });
});

test("a refused status change goes back to the saved one", async () => {
  const { instance, memory, reloads } = setup();
  await memory.gateway.deleteTask("t1"); // gone meanwhile
  instance.status = "done";
  await instance.changeStatus();
  assert.equal(instance.status, "in_progress");
  assert.equal(instance.taskError.code, Codes.TICKET_NOT_FOUND);
  assert.equal(reloads(), 0);
});

test("edits the title and description in place", async () => {
  const { instance, memory } = setup();
  instance.startEditTask();
  assert.equal(instance.draftTitle, "Add SSE feed");
  instance.draftTitle = "  ";
  assert.equal(instance.cannotSaveTask, true);
  instance.draftTitle = "Add the SSE feed";
  instance.draftDescription = "Follow every session change.";
  await instance.save();
  assert.equal(instance.editingTask, false);
  assert.deepEqual([instance.title, memory.tasks()[0].description], ["Add the SSE feed", "Follow every session change."]);
  assert.equal(memory.tasks()[0].status, "in_progress", "editingTask keeps the status");
});

test("a task with sessions is not deleted", async () => {
  const { instance, memory } = setup({ sessions: [{ ticketId: "t1", status: "idle" }] });
  await instance.askDeleteTask();
  assert.equal(instance.confirmingTaskDelete, false);
  assert.ok(instance.taskDeleteProblem.includes("running session"));
  assert.equal(memory.tasks().length, 1);
});

test("a task without sessions is deleted after confirming, then the project opens", async () => {
  const { instance, memory, visited } = setup();
  await instance.askDeleteTask();
  assert.equal(instance.confirmingTaskDelete, true);
  instance.cancelDeleteTask();
  assert.equal(instance.confirmingTaskDelete, false);
  await instance.askDeleteTask();
  await instance.removeTask();
  assert.deepEqual(memory.tasks(), []);
  assert.deepEqual(visited, ["/projects/p1"]);
});

// ── a status moved elsewhere ──────────────────────────────────────────────

function liveSetup() {
  const task = makeTask({ architectSessionId: "s-arch" });
  const memory = memoryTasks({ projects: ["p1"], tasks: [task] });
  const store = { tasks: [task], statusChanges: {} };
  const clock = { now: () => new Date("2026-10-02T14:02:00Z") };
  const el = seededElement(taskDTO(task));
  const mounted = mount(taskEditor({ gateway: memory.gateway, navigate: () => {}, reload: () => {}, store, clock }), { el });
  mounted.instance.init();
  return { ...mounted, store };
}

test("the architect moving the task shows who, when and why, and the picker follows", () => {
  const { instance, store } = liveSetup();
  assert.equal(instance.statusLine, "", "nothing seen yet: a reload starts without it");
  store.statusChanges = { t1: { taskId: "t1", status: "review", reason: "Server and tools merged", by: "session", bySessionId: "s-arch", at: new Date("2026-10-02T14:00:00Z") } };
  store.tasks = [makeTask({ status: "review", architectSessionId: "s-arch" })];
  instance.follow(instance.storedStatus); // what Alpine's $watch on storedStatus does
  assert.deepEqual([instance.statusLine, instance.statusReason], ["Moved to review by the architect · 2m", "“Server and tools merged”"]);
  assert.deepEqual([instance.status, instance.task.status], ["review", "review"]);
  instance.destroy();
});

test("the picker does not follow the echo of the page's own move", () => {
  const { instance } = liveSetup();
  instance.movingStatus = true;
  instance.follow("done");
  assert.equal(instance.task.status, "in_progress");
  instance.destroy();
});

test("a done task offers Archive; archiving it reloads the page, which then shows the archived banner", async () => {
  const { instance, memory, reloads } = setup({ task: makeTask({ status: "done" }) });
  assert.deepEqual([instance.canArchive, instance.isArchived], [true, false]);
  await instance.archiveTask();
  assert.ok(memory.tasks()[0].archivedAt > 0);
  assert.equal(reloads(), 1);
});

test("a task that is not done offers no Archive", () => {
  const { instance } = setup();
  assert.equal(instance.canArchive, false);
});

test("an archived task says when it was archived and restores to Done", async () => {
  const archivedAt = new Date(2026, 9, 18, 12).getTime();
  const { instance, memory, reloads } = setup({ task: makeTask({ status: "done", archivedAt }) });
  assert.deepEqual([instance.isArchived, instance.canArchive, instance.archivedWhen], [true, false, "Archived Oct 18."]);
  await instance.restoreTask();
  assert.deepEqual([memory.tasks()[0].archivedAt, reloads()], [0, 1]);
});

test("a refused archive shows the coded error and stays", async () => {
  const { instance, memory, reloads } = setup({ task: makeTask({ status: "done" }) });
  memory.gateway.archiveTask = async () => {
    throw new StructuredError(Codes.TASK_NOT_DONE, "only a done task can be archived", 409);
  };
  await instance.archiveTask();
  assert.deepEqual([instance.taskError.code, reloads(), instance.archiving], [Codes.TASK_NOT_DONE, 0, false]);
});

// ── the task note: a popover from the title row ──────────────────────────

/** The title row's buttons the surface returns the focus to, in a page the test owns. */
function titleRow() {
  const row = document.createElement("div");
  row.innerHTML = '<button id="task-note-button">Description</button><button id="task-edit-button">Edit</button>';
  document.body.append(row);
  return { row, note: row.querySelector("#task-note-button"), edit: row.querySelector("#task-edit-button") };
}
const opened = (mounted) => mounted.dispatched.filter((e) => e.name === "dialog-open");
const dismissed = (mounted) => mounted.dispatched.filter((e) => e.name === "dialog-dismiss");

test("the Description button shows when the task has a description, and only then", () => {
  assert.equal(setup().instance.hasDescription, true);
  assert.equal(setup({ task: makeTask({ description: "  " }) }).instance.hasDescription, false);
});

test("a page loads with the note closed, and opening it moves nothing the page stores", () => {
  const row = titleRow();
  try {
    const m = setup();
    assert.deepEqual([m.instance.noteOpen, m.instance.editingTask], [false, false]);
    m.instance.openNote();
    assert.equal(m.instance.noteOpen, true);
    assert.deepEqual(opened(m).map((e) => [e.detail.id, e.detail.anchor]), [["task-note", row.note]], "opens under the Description button");
  } finally {
    row.row.remove();
  }
});

test("the note's close, from Esc, Close or a press outside, leaves it closed and read-only again", () => {
  const row = titleRow();
  try {
    const m = setup();
    m.instance.startEditTask();
    assert.equal(m.instance.noteOpen, true);
    m.instance.noteClosed({ detail: { id: "task-note" } });
    assert.deepEqual([m.instance.noteOpen, m.instance.editingTask], [false, false]);
    m.instance.openNote();
    m.instance.noteClosed({ detail: { id: "another-dialog" } });
    assert.equal(m.instance.noteOpen, true, "another dialog closing is not this one");
  } finally {
    row.row.remove();
  }
});

test("Edit from the note or the title row shows the form in the same surface", () => {
  const row = titleRow();
  try {
    const m = setup();
    m.instance.openNote();
    m.instance.startEditTask();
    assert.deepEqual([m.instance.editingTask, m.instance.draftTitle, m.instance.draftDescription], [true, "Add SSE feed", "Stream session changes to clients."]);
    assert.equal(opened(m).length, 1, "already open: it does not open again");
    const fromRow = setup();
    fromRow.instance.startEditTask();
    assert.deepEqual([fromRow.instance.noteOpen, fromRow.instance.editingTask], [true, true]);
    assert.equal(opened(fromRow)[0].detail.anchor, row.note, "the focus comes back to Description");
  } finally {
    row.row.remove();
  }
});

test("a task with no description has no Description button, and Edit opens the surface in its edit state", () => {
  const row = titleRow();
  row.note.remove();
  try {
    const m = setup({ task: makeTask({ description: "" }) });
    assert.equal(m.instance.hasDescription, false);
    m.instance.startEditTask();
    assert.deepEqual([m.instance.noteOpen, m.instance.editingTask], [true, true]);
    assert.equal(opened(m)[0].detail.anchor, row.edit, "anchored on the title row's Edit");
    m.instance.cancelEditTask();
    assert.equal(dismissed(m).length, 1, "nothing to read: Cancel closes it");
  } finally {
    row.row.remove();
  }
});

test("Save closes the surface and the next opening reads the new description; Cancel returns to the read view", async () => {
  const row = titleRow();
  try {
    const m = setup();
    m.instance.startEditTask();
    m.instance.cancelEditTask();
    assert.deepEqual([m.instance.editingTask, m.instance.noteOpen, dismissed(m).length], [false, true, 0], "Cancel keeps the note open to read");
    m.instance.startEditTask();
    m.instance.draftDescription = "Follow every session change.";
    await m.instance.save();
    assert.deepEqual([m.instance.editingTask, m.instance.description, dismissed(m).length], [false, "Follow every session change.", 1]);
    assert.deepEqual(dismissed(m)[0].detail, { id: "task-note" });
  } finally {
    row.row.remove();
  }
});

test("a refused Save keeps the form open with the error", async () => {
  const row = titleRow();
  try {
    const m = setup();
    m.instance.startEditTask();
    await m.memory.gateway.deleteTask("t1");
    await m.instance.save();
    assert.deepEqual([m.instance.editingTask, dismissed(m).length, m.instance.taskError.code], [true, 0, Codes.TICKET_NOT_FOUND]);
  } finally {
    row.row.remove();
  }
});

test("a description or title changed over the feed shows in the open note, but never over a draft", () => {
  const store = { tasks: [makeTask()], statusChanges: {} };
  const { instance } = setup({ store });
  store.tasks = [makeTask({ title: "Add the feed", description: "Rewritten by a session." })];
  instance.followText(instance.storedText);
  assert.deepEqual([instance.title, instance.description], ["Add the feed", "Rewritten by a session."]);
  instance.startEditTask();
  instance.draftDescription = "my words";
  store.tasks = [makeTask({ description: "Another rewrite." })];
  instance.followText(instance.storedText);
  assert.deepEqual([instance.description, instance.draftDescription], ["Rewritten by a session.", "my words"], "the person's draft is not clobbered");
});

// ── the Token savers section of the Edit form ─────────────────────────────

const activationsOf = (values = {}) => ({
  level: "task",
  ownerId: "t1",
  activations: ["rtk", "caveman", "codegraph"].map((saver) => ({ saver, value: values[saver] ?? "inherit", summary: `${values[saver] ?? "inherit"} — resolves on (project)` })),
});

function saversSetup({ values, failOn = "" } = {}) {
  const task = makeTask();
  const memory = memoryTasks({ projects: ["p1"], tasks: [task] });
  const tokens = memoryTokenSavers({ activations: [activationsOf(values)] });
  const order = [];
  const tasksGateway = { ...memory.gateway, updateTask: (...a) => (order.push("task"), memory.gateway.updateTask(...a)) };
  const tokenSavers = {
    ...tokens.gateway,
    setActivation: (level, owner, saver, value) => {
      order.push(`${saver}=${value}`);
      if (saver === failOn) return Promise.reject(new StructuredError(Codes.INVALID_INPUT, "INVALID_INPUT: nope", 400));
      return tokens.gateway.setActivation(level, owner, saver, value);
    },
  };
  const el = seededElement(taskDTO(task));
  const script = el.ownerDocument.createElement("script");
  script.id = "savers-seed";
  script.textContent = JSON.stringify({ level: "task", owner_id: "t1", activations: activationsOf(values).activations });
  el.ownerDocument.body.append(script);
  el.dataset.saversSeed = "savers-seed";
  const mounted = mount(taskEditor({ gateway: tasksGateway, tokenSavers, navigate: () => {}, reload: () => {} }), { el });
  mounted.instance.init();
  return { ...mounted, order, tokens };
}

test("the Edit form lists the task's savers with what each resolves to; a task without them has no section", () => {
  const { instance } = saversSetup({ values: { caveman: "on" } });
  assert.equal(instance.hasSavers, true);
  assert.deepEqual(instance.savers.map((s) => [s.saver, s.value, s.draft, s.summary]), [
    ["rtk", "inherit", "inherit", "inherit — resolves on (project)"],
    ["caveman", "on", "on", "on — resolves on (project)"],
    ["codegraph", "inherit", "inherit", "inherit — resolves on (project)"],
  ]);
  assert.equal(setup().instance.hasSavers, false);
});

test("choosing a saver writes nothing until Save; Save writes the task, then only the changed savers", async () => {
  const { instance, order } = saversSetup();
  instance.startEditTask();
  instance.savers[0].draft = "on";
  instance.savers[2].draft = "off";
  instance.draftTitle = "Add the SSE feed";
  assert.deepEqual(order, [], "nothing is written on change");
  await instance.save();
  assert.deepEqual(order, ["task", "rtk=on", "codegraph=off"], "the task first; caveman is unchanged and writes nothing");
  assert.deepEqual(instance.savers.map((s) => [s.value, s.draft]), [["on", "on"], ["inherit", "inherit"], ["off", "off"]]);
  assert.equal(instance.savers[0].summary, "on (task)", "the line follows the server's answer");
  assert.equal(instance.editingTask, false);
});

test("Save with no saver changed writes no activation", async () => {
  const { instance, order } = saversSetup();
  instance.startEditTask();
  await instance.save();
  assert.deepEqual(order, ["task"]);
});

test("Cancel drops every change, the savers' too", () => {
  const { instance, order } = saversSetup();
  instance.startEditTask();
  instance.savers[1].draft = "off";
  instance.cancelEditTask();
  assert.deepEqual(instance.savers.map((s) => s.draft), ["inherit", "inherit", "inherit"]);
  instance.startEditTask();
  assert.equal(instance.savers[1].draft, "inherit", "reopening starts from what is saved");
  assert.deepEqual(order, []);
});

test("a saver write that fails leaves the form open with its error; what was written stays written", async () => {
  const { instance, order } = saversSetup({ failOn: "caveman" });
  instance.startEditTask();
  instance.savers[0].draft = "on";
  instance.savers[1].draft = "off";
  await instance.save();
  assert.deepEqual(order, ["task", "rtk=on", "caveman=off"]);
  assert.equal(instance.editingTask, true, "the form stays open");
  assert.equal(instance.saversError.code, Codes.INVALID_INPUT);
  assert.deepEqual(instance.savers.map((s) => [s.value, s.draft]), [["on", "on"], ["inherit", "off"], ["inherit", "inherit"]], "rtk landed, caveman keeps the person's choice");
  instance.cancelEditTask();
  assert.equal(instance.saversError, null, "Cancel clears the error");
});

test("a task that fails to save writes no saver", async () => {
  const { instance, order, memory } = saversSetup();
  void memory;
  instance.startEditTask();
  instance.savers[0].draft = "on";
  instance.draftTitle = " ";
  await instance.save();
  assert.deepEqual(order, [], "the form refuses a blank title before anything is sent");
});
