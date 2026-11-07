import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { toService } from "../../infrastructure/dto.js";
import { fakeServices, listDTO, runDTO, serviceDTO } from "../../testing/fakeGateway.js";
import { serviceForm } from "./serviceForm.js";

file("services/presentation/components/serviceForm");

const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };

function open(service = null, gateway = fakeServices({ list: listDTO([serviceDTO({ id: "s1" })]) })) {
  const { instance, dispatched, tick } = mount(serviceForm({ gateway }), { el: { querySelector: () => null } });
  instance.setup(service, "p1");
  return { form: instance, gateway, dispatched, tick };
}
const editing = (o = {}) => toService(serviceDTO({ id: "s1", name: "api", repository_id: "r1", export_repository_env: true, command: { line: "make air", subdir: "", ports: [8080] }, ...o }));

test("a new service opens empty as a command service, and Create service is not ready", () => {
  const { form } = open();
  assert.equal(form.title, "New service");
  assert.equal(form.saveLabel, "Create service");
  assert.equal(form.isCommand, true);
  assert.equal(form.kindLocked, false);
  assert.equal(form.ready, false);
});

test("choosing Image shows the image fields instead of the command's", () => {
  const { form } = open();
  form.chooseKind({ currentTarget: { value: "image" } });
  assert.deepEqual([form.isImage, form.isCommand], [true, false]);
});

test("Create sends the draft as the API's input and tells the page what was created", async () => {
  const { form, gateway, dispatched } = open();
  form.draft.name = "api";
  form.draft.line = "make air";
  form.draft.ports[0].value = "8080";
  await form.save();
  assert.equal(gateway.saves.length, 1);
  const [verb, projectId, input] = gateway.saves[0];
  assert.deepEqual([verb, projectId, input.name, input.command.line, input.command.ports], ["create", "p1", "api", "make air", [8080]]);
  assert.deepEqual(dispatched.map((d) => d.name), ["service-saved"]);
  assert.equal(dispatched[0].detail.service.name, "api");
  assert.equal(dispatched[0].detail.created, true);
});

test("Create does nothing while a save is under way", async () => {
  const gateway = fakeServices();
  let release;
  gateway.save.create = () => new Promise((resolve) => { release = resolve; });
  const { form } = open(null, gateway);
  form.draft.name = "api";
  form.draft.line = "x";
  const first = form.save();
  await form.save();
  assert.equal(gateway.saves.length, 1);
  assert.equal(form.saving, true);
  release(toService(serviceDTO()));
  await first;
});

test("an INVALID_INPUT refusal lands under the field it names, marks it invalid and keeps the form", async () => {
  const gateway = fakeServices();
  gateway.save.create = async () => { throw new StructuredError("INVALID_INPUT", "command.subdir must be relative and stay inside the repository"); };
  const { form, dispatched } = open(null, gateway);
  form.draft.name = "api";
  form.draft.line = "make";
  form.draft.subdir = "../x";
  await form.save();
  assert.equal(form.fieldMessage("subdir"), "command.subdir must be relative and stay inside the repository");
  assert.equal(form.invalid("subdir"), true);
  assert.equal(form.invalid("name"), false);
  assert.equal(form.banner, null);
  assert.equal(dispatched.length, 0);
  assert.equal(form.saving, false);
});

test("a refusal naming a row marks that row, and typing in the field clears its message", async () => {
  const gateway = fakeServices();
  gateway.save.create = async () => { throw new StructuredError("INVALID_INPUT", "command.ports[1] must be a port from 1 to 65535"); };
  const { form } = open(null, gateway);
  form.draft.name = "api";
  form.draft.line = "make";
  await form.save();
  assert.equal(form.rowInvalid("ports", 1), true);
  assert.equal(form.rowInvalid("ports", 0), false);
  form.touch("ports");
  assert.equal(form.invalid("ports"), false);
});

test("SERVICE_NAME_TAKEN and REPOSITORY_NOT_FOUND show as a form banner with the code", async () => {
  const gateway = fakeServices();
  gateway.save.create = async () => { throw new StructuredError("SERVICE_NAME_TAKEN", "api is already used by another service of this project"); };
  const { form } = open(null, gateway);
  form.draft.name = "api";
  form.draft.line = "make";
  await form.save();
  assert.equal(form.banner.code, "SERVICE_NAME_TAKEN");
  assert.equal(form.banner.message, "api is already used by another service of this project");
});

test("the first invalid field takes focus", async () => {
  const gateway = fakeServices();
  gateway.save.create = async () => { throw new StructuredError("INVALID_INPUT", "name must be 1 to 64 characters"); };
  const focused = [];
  const el = { querySelector: (sel) => (sel === "#sf-name" ? { focus: () => focused.push("name") } : null) };
  const { instance: form, tick } = mount(serviceForm({ gateway }), { el });
  form.setup(null, "p1");
  tick();
  assert.deepEqual(focused, ["name"], "a form opens with the name field focused");
  focused.length = 0;
  form.draft.name = "X";
  form.draft.line = "make";
  await form.save();
  tick();
  assert.deepEqual(focused, ["name"]);
});

test("editing opens with the service's values, locks the kind and says Save", () => {
  const { form } = open(editing());
  assert.equal(form.title, "Edit api");
  assert.equal(form.kindLocked, true);
  assert.equal(form.saveLabel, "Save");
  assert.equal(form.draft.name, "api");
  assert.equal(form.draft.line, "make air");
  assert.equal(form.ready, true);
});

test("editing a running service says Restart to apply, and Save keeps the service running", async () => {
  const { form, gateway, dispatched } = open(editing({ status: "running", run: runDTO() }));
  assert.equal(form.restartBanner, true);
  assert.equal(form.saveState, "Changes apply at the next start");
  gateway.save.update = async (id, input) => ({ service: toService(serviceDTO({ id, name: input.name, status: "running", run: runDTO() })), restartNeeded: true });
  await form.save();
  assert.equal(dispatched[0].detail.restartNeeded, true);
  assert.equal(dispatched[0].detail.created, false);
  assert.deepEqual(gateway.actions, []);
});

test("Save and restart saves, then restarts the service", async () => {
  const { form, gateway, dispatched } = open(editing({ status: "running", run: runDTO() }));
  gateway.save.update = async (id, input) => ({ service: toService(serviceDTO({ id, name: input.name, status: "running", run: runDTO() })), restartNeeded: true });
  gateway.answer.restart = async (id) => gateway.byId(id, "running");
  await form.saveAndRestart();
  assert.deepEqual(gateway.saves.map((s) => s[0]), ["update"]);
  assert.deepEqual(gateway.actions, ["restart:s1"]);
  assert.deepEqual(dispatched.map((d) => d.name), ["service-saved"]);
});

test("a restart refused after Save and restart is shown, and the saved change is kept", async () => {
  const { form, gateway, dispatched } = open(editing({ status: "running", run: runDTO() }));
  gateway.save.update = async (id, input) => ({ service: toService(serviceDTO({ id, name: input.name, status: "running", run: runDTO() })), restartNeeded: true });
  gateway.answer.restart = async () => { throw new StructuredError("SANDBOX_UNAVAILABLE", "The sandbox is unavailable"); };
  await form.saveAndRestart();
  assert.equal(form.banner.code, "SANDBOX_UNAVAILABLE");
  assert.equal(dispatched.length, 1, "the page learns of the save");
});

test("choosing Image says so in the form when the runtime was not found, and Command does not", () => {
  const { instance: form } = mount(serviceForm({ gateway: fakeServices() }), { el: { querySelector: () => null } });
  form.setup(null, "p1", { name: "docker", available: false });
  assert.equal(form.hasRuntimeNotice, false);
  form.chooseKind({ currentTarget: { value: "image" } });
  assert.equal(form.runtimeNotice, "docker was not found on this machine. Image services cannot start until it is installed, or you choose another runtime in the server settings.");
});

test("the .env export is only offered once a repository is picked", () => {
  const { form } = open();
  assert.equal(form.canExport, false);
  form.draft.repositoryId = "r1";
  assert.equal(form.canExport, true);
  assert.equal(form.exportHint, "Adds the .env saved for the repository (Settings → Repositories) under the variables above.");
});

test("port, argument and mapping rows are added and removed", () => {
  const { form } = open();
  form.addPort();
  assert.equal(form.draft.ports.length, 2);
  form.removeRow({ currentTarget: { dataset: { list: "ports", index: "0" } } });
  assert.equal(form.draft.ports.length, 1);
  form.chooseKind({ currentTarget: { value: "image" } });
  form.addArg();
  form.addMapping();
  assert.deepEqual([form.draft.args.length, form.draft.mappings.length], [1, 2]);
});

test("Cancel closes the form without saving", () => {
  const { form, dispatched } = open();
  form.cancel();
  assert.deepEqual(dispatched.map((d) => d.name), ["service-form-close"]);
});

test("the env value is never put in the console", async () => {
  const logged = [];
  const orig = { log: console.log, error: console.error, warn: console.warn };
  console.log = console.error = console.warn = (...a) => logged.push(a.join(" "));
  try {
    const gateway = fakeServices();
    gateway.save.create = async () => { throw new StructuredError("INVALID_INPUT", "env line 1 is not KEY=VALUE"); };
    const { form } = open(null, gateway);
    form.draft.name = "api";
    form.draft.line = "make";
    form.draft.env = "SECRET_TOKEN=hunter2";
    await form.save();
  } finally {
    Object.assign(console, orig);
  }
  assert.equal(logged.some((l) => l.includes("hunter2")), false);
});
