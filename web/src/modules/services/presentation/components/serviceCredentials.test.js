import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { serviceCredentials } from "./serviceCredentials.js";

file("services/presentation/components/serviceCredentials");

const CREDS = [{ label: "user", value: "tester" }, { label: "password", value: "hunter2" }];

function open(credentials = CREDS) {
  const copied = [];
  const clipboard = { writeText: async (text) => { copied.push(text); } };
  const { instance } = mount(serviceCredentials({ clipboard }));
  instance.credentialsChanged(credentials);
  return { creds: instance, copied };
}
const row = (creds, label) => creds.rows.find((r) => r.label === label);
const target = (label) => ({ currentTarget: { dataset: { label } } });

test("each credential shows its label with a fixed mask that hides the value and its length", () => {
  const { creds } = open();
  assert.deepEqual(creds.rows.map((r) => r.label), ["user", "password"]);
  assert.equal(row(creds, "user").shown, row(creds, "password").shown);
  assert.equal(row(creds, "user").masked, true);
  assert.equal(JSON.stringify(creds.rows).includes("hunter2"), false);
  assert.equal(creds.summary, "2 credentials");
});

test("Show reveals one value and Hide masks it again; the button names the credential", () => {
  const { creds } = open();
  assert.equal(row(creds, "password").toggleName, "Show password");
  creds.toggle(target("password"));
  assert.deepEqual([row(creds, "password").shown, row(creds, "password").masked, row(creds, "password").toggleName], ["hunter2", false, "Hide password"]);
  assert.equal(row(creds, "user").masked, true);
  creds.toggle(target("password"));
  assert.equal(row(creds, "password").masked, true);
});

test("Copy puts the value on the clipboard without revealing it", async () => {
  const { creds, copied } = open();
  await creds.copy(target("password"));
  assert.deepEqual(copied, ["hunter2"]);
  assert.equal(row(creds, "password").masked, true);
  assert.equal(row(creds, "password").copyText, "Copied");
});

test("a service with none has no section; a change of the list drops what no longer exists", () => {
  const { creds } = open();
  creds.toggle(target("user"));
  creds.credentialsChanged([{ label: "password", value: "new" }]);
  assert.equal(creds.rows.length, 1);
  assert.equal(creds.revealed.user, undefined);
  creds.credentialsChanged([]);
  assert.equal(creds.hasRows, false);
});
