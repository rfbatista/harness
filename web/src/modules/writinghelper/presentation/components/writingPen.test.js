import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { stubGateway } from "../../testing/stub-gateway.js";
import { createCoordinator } from "../coordinator.js";
import { writingPen } from "./writingPen.js";

file("writinghelper/presentation/components/writingPen");

const settle = () => new Promise((r) => setTimeout(r, 0));

/** A pen's markup as the server paints it, over a field with `text`. */
function penEl({ kind = "task_description", text = "", siblings = [], available = true } = {}) {
  const root = document.createElement("div");
  Object.assign(root.dataset, {
    kind,
    noun: kind === "task_title" ? "title" : "description",
    label: kind === "task_title" ? "Title" : "Description",
    projectId: "p1",
    projectName: "harness",
    panelId: "pen-x",
    siblings: JSON.stringify(siblings),
    helperAvailable: String(available),
  });
  const wrap = document.createElement("div");
  wrap.className = "pen-field";
  const field = document.createElement(kind === "task_title" ? "input" : "textarea");
  field.value = text;
  wrap.append(field);
  root.append(wrap);
  document.body.append(root);
  return { root, field };
}

function setup(options = {}, gatewayScript = {}) {
  const { root, field } = penEl(options);
  const gateway = stubGateway(gatewayScript);
  const coordinator = options.coordinator ?? createCoordinator();
  const timers = [];
  const mounted = mount(writingPen({ gateway, coordinator, setTimeout: (fn) => timers.push(fn) && timers.length, clearTimeout: () => {}, matchMedia: () => ({ matches: false, addEventListener() {} }) }), { el: root });
  mounted.instance.init();
  return { ...mounted, gateway, field, root, coordinator, timers, done: () => root.remove() };
}

test("a pen on a field with text opens by reading and shows the questions", async () => {
  const { instance, gateway, done } = setup({ text: "rough text", siblings: [{ label: "Title", text: "A title" }] });
  instance.open();
  assert.equal(instance.showReading, true);
  await settle();
  assert.equal(instance.showQuestions, true);
  assert.equal(gateway.calls[0].step, "analyze");
  assert.equal(gateway.calls[0].project_id, "p1");
  assert.deepEqual(gateway.calls[0].siblings, [{ label: "Title", text: "A title" }]);
  assert.deepEqual(instance.reads, ["Description", "Title", "project harness"]);
  done();
});

test("an empty field starts at the question and offers the title when it has text", () => {
  const { root, field, instance, done } = setup({ siblings: [{ label: "Title", text: "A title" }] });
  instance.open();
  assert.equal(instance.showStart, true);
  assert.equal(instance.hasStartChip, true);
  assert.equal(instance.startChip, "Draft from the title");
  assert.equal(field.value, "");
  assert.ok(root.isConnected);
  done();
});

test("drafting streams into the panel and leaves the field alone until Use this", async () => {
  const { instance, gateway, field, done } = setup({ text: "rough text" }, { draft: "A clear brief." });
  instance.open();
  await settle();
  const first = instance.draft();
  assert.equal(instance.showDrafting, true);
  await first;
  assert.equal(instance.showProposal, true);
  assert.equal(instance.panel.draft, "A clear brief.");
  assert.equal(field.value, "rough text");
  assert.equal(gateway.calls[1].step, "draft");
  done();
});

test("Use this replaces the field, closes the panel, offers Undo and Undo restores the text", async () => {
  const { instance, field, done } = setup({ text: "rough text" }, { draft: "A clear brief." });
  const inputs = [];
  field.addEventListener("input", () => inputs.push(field.value));
  instance.open();
  await settle();
  await instance.draft();
  instance.use();
  assert.equal(field.value, "A clear brief.");
  assert.equal(instance.isOpen, false);
  assert.equal(instance.toastMessage, "Description replaced by the helper's draft");
  assert.equal(instance.toastCanUndo, true);
  assert.ok(inputs.length >= 1);
  instance.undo();
  assert.equal(field.value, "rough text");
  assert.equal(instance.hasToast, false);
  done();
});

test("the toast goes by itself after a while", async () => {
  const { instance, timers, done } = setup({ text: "rough text" }, { draft: "A clear brief." });
  instance.open();
  await settle();
  await instance.draft();
  instance.use();
  assert.equal(instance.hasToast, true);
  timers.at(-1)();
  assert.equal(instance.hasToast, false);
  done();
});

test("closing while the helper works drops its answer", async () => {
  const { instance, gateway, done } = setup({ text: "rough text" });
  const release = gateway.gate();
  instance.open();
  instance.close(true);
  release();
  await settle();
  assert.equal(instance.isOpen, false);
  done();
});

test("Stop during a draft goes back to the questions and keeps the answers", async () => {
  const { instance, gateway, done } = setup({ text: "rough text" });
  instance.open();
  await settle();
  instance.pickChip("q1", "Every field");
  const release = gateway.gate();
  instance.draft();
  assert.equal(instance.showDrafting, true);
  instance.stop();
  release();
  await settle();
  assert.equal(instance.showQuestions, true);
  assert.equal(instance.panel.answers.q1, "Every field");
  done();
});

test("typing in the field after a draft marks it stale", async () => {
  const { instance, field, done } = setup({ text: "rough text" });
  instance.open();
  await settle();
  await instance.draft();
  field.value = "rough text, edited";
  instance.fieldEdited();
  assert.equal(instance.showStale, true);
  assert.equal(instance.showProposal, false);
  done();
});

test("a failed step shows the error, keeps the answers, and Try again runs it again", async () => {
  const { instance, gateway, done } = setup({ text: "rough text" });
  instance.open();
  await settle();
  instance.pickChip("q1", "Every field");
  gateway.failNext("WRITING_HELPER_FAILED", "claude stopped");
  await instance.draft();
  assert.equal(instance.showError, true);
  assert.equal(instance.errorCode, "WRITING_HELPER_FAILED");
  assert.equal(instance.errorMessage, "claude stopped Your description is unchanged.");
  await instance.retry();
  assert.equal(instance.showProposal, true);
  assert.deepEqual(gateway.calls.at(-1).answers, [{ question: "Which fields get the pen first?", answer: "Every field" }]);
  done();
});

test("a refine sends the current draft and the request, and keeps the answers", async () => {
  const { instance, gateway, done } = setup({ text: "rough text" }, { draft: "Brief." });
  instance.open();
  await settle();
  instance.pickChip("q1", "Every field");
  await instance.draft();
  await instance.refineChip("Shorter");
  const last = gateway.calls.at(-1);
  assert.equal(last.draft, "Brief.");
  assert.equal(last.request, "Shorter");
  assert.equal(last.answers.length, 1);
  done();
});

test("a refused helper switches every pen off", async () => {
  const coordinator = createCoordinator();
  const a = setup({ text: "x", coordinator });
  const b = setup({ text: "y", coordinator });
  a.gateway.failNext("WRITING_HELPER_UNAVAILABLE", "not signed in", { fix_url: "/agent-isolation#credential" });
  a.instance.open();
  await settle();
  assert.equal(a.instance.penDisabled, true);
  assert.equal(b.instance.penDisabled, true);
  assert.equal(b.instance.penLabel, "Writing helper unavailable");
  assert.equal(a.instance.isOpen, false);
  assert.equal(a.instance.toastHasLink, true);
  a.done();
  b.done();
});

test("an unavailable pen does not open", () => {
  const { instance, gateway, done } = setup({ text: "x", available: false });
  instance.open();
  assert.equal(instance.isOpen, false);
  assert.equal(gateway.calls.length, 0);
  assert.equal(instance.penTitle, "Writing helper unavailable");
  done();
});

test("a pen on a disabled field is disabled", () => {
  const { instance, field, done } = setup({ text: "x" });
  field.disabled = true;
  instance.fieldDisabled = field.disabled;
  assert.equal(instance.penDisabled, true);
  done();
});

test("opening a second pen closes the first and discards its draft", async () => {
  const coordinator = createCoordinator();
  const a = setup({ text: "x", coordinator });
  const b = setup({ text: "y", coordinator });
  a.instance.open();
  await settle();
  b.instance.open();
  assert.equal(a.instance.isOpen, false);
  assert.equal(b.instance.isOpen, true);
  a.done();
  b.done();
});

test("Ctrl+J toggles the panel from the field, and Esc closes it and stops the key", async () => {
  const { instance, field, done } = setup({ text: "rough text" });
  const press = (init) => {
    const e = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init });
    field.dispatchEvent(e);
    return e;
  };
  const opened = press({ key: "j", ctrlKey: true });
  assert.equal(opened.defaultPrevented, true);
  assert.equal(instance.isOpen, true);
  await settle();
  const esc = press({ key: "Escape" });
  assert.equal(esc.defaultPrevented, true);
  assert.equal(instance.isOpen, false);
  assert.equal(field.value, "rough text");
  done();
});

test("the field's own handlers do not see Esc while the helper is open", async () => {
  const { instance, field, done } = setup({ text: "rough text" });
  let seen = 0;
  field.addEventListener("keydown", (e) => e.key === "Escape" && seen++);
  instance.open();
  await settle();
  field.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  assert.equal(seen, 0);
  done();
});

test("a title pen reads its options and the keys 1 to 3 pick one", async () => {
  const { instance, field, gateway, done } = setup({ kind: "task_title", text: "Old title" });
  instance.open();
  assert.equal(instance.showReading, true);
  await settle();
  assert.equal(gateway.calls[0].step, "titles");
  assert.equal(instance.showTitles, true);
  assert.equal(instance.titleRows.length, 3);
  instance.windowKey({ key: "2", target: document.body, preventDefault() {} });
  assert.equal(field.value, "Title option 2");
  assert.equal(instance.toastMessage, "Title replaced by the helper's pick");
  done();
});

test("a digit typed into a text input does not pick a title", async () => {
  const { instance, field, done } = setup({ kind: "task_title", text: "Old title" });
  instance.open();
  await settle();
  instance.windowKey({ key: "1", target: field, preventDefault() {} });
  assert.equal(field.value, "Old title");
  assert.equal(instance.isOpen, true);
  done();
});

test("Three more sends the titles already offered", async () => {
  const { instance, gateway, done } = setup({ kind: "task_title", text: "" });
  instance.open();
  await settle();
  await instance.threeMore();
  assert.deepEqual(gateway.calls.at(-1).exclude, ["Title option 1", "Title option 2", "Title option 3"]);
  assert.equal(instance.titleRows[0].text, "Title option 4");
  done();
});

test("a text over the helper's limit fails before anything is sent", async () => {
  const { instance, gateway, done } = setup({ text: "x".repeat(20_001) });
  instance.open();
  await settle();
  assert.equal(instance.showError, true);
  assert.equal(instance.errorCode, "INVALID_INPUT");
  assert.equal(gateway.calls.length, 0);
  done();
});

test("the diff view paints insertions, removals and gaps and the clean view only flags itself", async () => {
  const { instance, done } = setup({ text: "rough text" }, { draft: "A clear brief [open: owner]" });
  instance.open();
  await settle();
  await instance.draft();
  const el = document.createElement("div");
  instance.paintDraft(el);
  assert.equal(el.querySelectorAll("ins").length > 0, true);
  assert.equal(el.querySelectorAll("del").length > 0, true);
  assert.equal(el.querySelector(".gap").textContent, "[open: owner]");
  assert.equal(el.dataset.view, "changes");
  instance.showClean();
  instance.paintDraft(el);
  assert.equal(el.dataset.view, "clean");
  done();
});

test("StructuredError from the gateway keeps its code in the panel", async () => {
  const { instance, gateway, done } = setup({ text: "rough" });
  gateway.runStep = async () => { throw new StructuredError("NETWORK", "The harness server is not reachable."); };
  instance.open();
  await settle();
  assert.equal(instance.errorCode, "NETWORK");
  done();
});
