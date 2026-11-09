import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { fakeServices } from "../../testing/fakeGateway.js";
import { serviceVariables } from "./serviceVariables.js";

file("services/presentation/components/serviceVariables");

const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };
const VARS = {
  run_id: "r1",
  variables: [
    { name: "LOG_LEVEL", value: "debug", source: "override", by: "go-developer · session abcdef12" },
    { name: "PORT", value: "8080", source: "service", by: "" },
    { name: "TOKEN", value: "s3cret", source: "repository_env", by: "" },
  ],
};

async function open({ variables = VARS, runId = "r1" } = {}) {
  const gateway = fakeServices();
  gateway.variables = variables;
  const el = { dataset: { serviceId: "s1" } };
  const { instance } = mount(serviceVariables({ gateway }), { el });
  instance.init();
  instance.runChanged(runId);
  await settle();
  return { vars: instance, gateway };
}
const row = (vars, name) => vars.rows.find((r) => r.name === name);

test("the variables of the run are read when the panel opens, sorted as the server sent them, each with its source", async () => {
  const { vars, gateway } = await open();
  assert.deepEqual(gateway.variableReads, ["s1"]);
  assert.deepEqual(vars.rows.map((r) => [r.name, r.source]), [
    ["LOG_LEVEL", "override by go-developer · session abcdef12"],
    ["PORT", "service"],
    ["TOKEN", "repository .env"],
  ]);
  assert.equal(vars.summary, "3 variables");
});

test("values are masked with a fixed mask that does not show their length", async () => {
  const { vars } = await open();
  assert.equal(row(vars, "PORT").shown, row(vars, "TOKEN").shown);
  assert.equal(row(vars, "PORT").masked, true);
  assert.equal(JSON.stringify(vars.rows).includes("s3cret"), false, "the value is not in the rows until it is revealed");
});

test("a row is revealed on its own, and hidden again; the button names the variable", async () => {
  const { vars } = await open();
  assert.equal(row(vars, "TOKEN").toggleName, "Reveal TOKEN");
  vars.toggle({ currentTarget: { dataset: { name: "TOKEN" } } });
  assert.deepEqual([row(vars, "TOKEN").shown, row(vars, "TOKEN").masked, row(vars, "TOKEN").toggleName], ["s3cret", false, "Hide TOKEN"]);
  assert.equal(row(vars, "PORT").masked, true);
  vars.toggle({ currentTarget: { dataset: { name: "TOKEN" } } });
  assert.equal(row(vars, "TOKEN").masked, true);
});

test("Reveal all shows every value, and becomes Hide all", async () => {
  const { vars } = await open();
  assert.equal(vars.allLabel, "Reveal all");
  vars.toggleAll();
  assert.deepEqual(vars.rows.map((r) => r.shown), ["debug", "8080", "s3cret"]);
  assert.equal(vars.allLabel, "Hide all");
  vars.toggleAll();
  assert.equal(vars.rows.every((r) => r.masked), true);
});

test("revealing every row by hand makes the toggle say Hide all", async () => {
  const { vars } = await open();
  for (const name of ["LOG_LEVEL", "PORT", "TOKEN"]) vars.toggle({ currentTarget: { dataset: { name } } });
  assert.equal(vars.allLabel, "Hide all");
});

test("a new run reads the variables again and hides everything", async () => {
  const { vars, gateway } = await open();
  vars.toggleAll();
  gateway.variables = { run_id: "r2", variables: [{ name: "PORT", value: "9090", source: "service", by: "" }] };
  vars.runChanged("r2");
  await settle();
  assert.deepEqual(gateway.variableReads, ["s1", "s1"]);
  assert.deepEqual(vars.rows.map((r) => [r.name, r.masked]), [["PORT", true]]);
});

test("the same run is not read again", async () => {
  const { vars, gateway } = await open();
  vars.runChanged("r1");
  await settle();
  assert.equal(gateway.variableReads.length, 1);
});

test("a service that never ran says so and reads nothing", async () => {
  const { vars, gateway } = await open({ runId: "" });
  assert.deepEqual(gateway.variableReads, []);
  assert.equal(vars.neverRan, true);
});

test("a run with no variables says so", async () => {
  const { vars } = await open({ variables: { run_id: "r1", variables: [] } });
  assert.deepEqual([vars.empty, vars.summary], [true, "no variables"]);
});

test("a read that fails shows the error, and Try again reads it", async () => {
  const gateway = fakeServices();
  gateway.variablesError = new StructuredError("NETWORK", "The harness server is not reachable.");
  const { instance } = mount(serviceVariables({ gateway }), { el: { dataset: { serviceId: "s1" } } });
  instance.init();
  instance.runChanged("r1");
  await settle();
  assert.equal(instance.error.code, "NETWORK");
  assert.equal(instance.rows.length, 0);
  gateway.variablesError = null;
  gateway.variables = VARS;
  await instance.retry();
  assert.equal(instance.error, null);
  assert.equal(instance.rows.length, 3);
});

test("an answer for a run that is no longer the current one is dropped", async () => {
  const gateway = fakeServices();
  let release;
  gateway.getRunVariables = () => new Promise((resolve) => { release = () => resolve({ runId: "r1", variables: [{ name: "OLD", value: "1", source: "service", by: "" }] }); });
  const { instance } = mount(serviceVariables({ gateway }), { el: { dataset: { serviceId: "s1" } } });
  instance.init();
  instance.runChanged("r1");
  const first = release;
  gateway.getRunVariables = async () => ({ runId: "r2", variables: [] });
  instance.runChanged("r2");
  await settle();
  first();
  await settle();
  assert.equal(instance.rows.length, 0);
});

test("values never reach the console or the browser's storage", async () => {
  const seen = [];
  const original = { log: console.log, error: console.error, warn: console.warn };
  console.log = console.error = console.warn = (...a) => seen.push(a);
  try {
    const { vars } = await open();
    vars.toggleAll();
  } finally {
    Object.assign(console, original);
  }
  assert.deepEqual(seen, []);
});

test("the section opens and closes", async () => {
  const { vars } = await open();
  assert.equal(vars.opened, false);
  vars.toggleSection();
  assert.equal(vars.opened, true);
});
