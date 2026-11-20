import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { projectAgentCredential } from "./projectAgentCredential.js";

file("agentcredential/presentation/projectAgentCredential");

/** A gateway that keeps one credential per project, as the server does. */
function fakeGateway({ refuse = false } = {}) {
  const own = new Map();
  const calls = [];
  const status = (id) => (own.has(id) ? { set: true, kind: own.get(id), updated: "2026-11-20T10:00:00Z" } : { set: false, kind: "", updated: "" });
  return {
    calls,
    getForProject: async (id) => status(id),
    setForProject: async (id, kind, value) => {
      calls.push(["set", id, kind, value]);
      if (refuse || value.trim() === "") throw new StructuredError(Codes.INVALID_INPUT, "Paste a token or key.");
      own.set(id, kind);
      return status(id);
    },
    removeForProject: async (id) => {
      calls.push(["remove", id]);
      own.delete(id);
      return status(id);
    },
  };
}

function setup({ seed = { project_id: "p1", set: false, kind: "", updated: "", global: { set: true, kind: "oauth" } }, gateway = fakeGateway() } = {}) {
  const mounted = mount(projectAgentCredential({ gateway }), { el: seededElement(seed) });
  const field = { value: "" };
  Object.defineProperty(mounted.instance, "$refs", { value: { value: field } });
  mounted.instance.init();
  mounted.tick();
  return { ...mounted, gateway, field };
}

test("starts from the seed: a project without its own credential says what it falls back to", () => {
  const { instance } = setup({ seed: { project_id: "p1", set: false, kind: "", updated: "", effective: { source: "global_pasted", kind: "oauth", account: "", subscription: "" } } });
  assert.deepEqual([instance.isSet, instance.chipText, instance.pasteLabel, instance.pasteTone], [false, "Global", "Paste a token or key", "primary"]);
  assert.equal(instance.fallbackLine, "Uses the global pasted OAuth token.");
});

test("the fallback line names the global login's account, or says agents cannot start", () => {
  const login = setup({ seed: { project_id: "p1", set: false, kind: "", updated: "", effective: { source: "global_login", kind: "login", account: "ana@example.com", subscription: "max" } } });
  assert.equal(login.instance.fallbackLine, "Uses the global login: ana@example.com, max");
  const none = setup({ seed: { project_id: "p1", set: false, kind: "", updated: "", effective: { source: "none", kind: "", account: "", subscription: "" } } });
  assert.equal(none.instance.fallbackLine, "No credential: agents cannot start.");
});

test("a project login asks before a pasted credential replaces it, and a pasted one asks before a sign-in", () => {
  const login = setup({ seed: { project_id: "p1", set: true, kind: "login", updated: "", account: "ana@example.com", subscription: "max", can_sign_in: true } });
  assert.deepEqual([login.instance.signedIn, login.instance.chipText, login.instance.isPasted, login.instance.signInShown], [true, "Signed in", false, false]);
  login.instance.askPaste();
  assert.deepEqual([login.instance.replacing, login.instance.pasting], ["paste", false]);
  login.instance.keepReplacing();
  const pasted = setup({ seed: { project_id: "p1", set: true, kind: "oauth", updated: "2026-11-01T00:00:00Z", can_sign_in: true } });
  pasted.instance.askSignIn();
  assert.equal(pasted.instance.replacing, "signin");
});

test("a pasted value is saved for this project only, the field is cleared, and the kind is shown", async () => {
  const { instance, gateway, field } = setup();
  instance.startPaste();
  field.value = "sk-ant-api03-hush";
  instance.detectKind({ target: field });
  assert.equal(instance.chosen, "api_key");
  await instance.save();
  assert.deepEqual(gateway.calls, [["set", "p1", "api_key", "sk-ant-api03-hush"]]);
  assert.equal(field.value, "");
  assert.deepEqual([instance.isSet, instance.kindName, instance.savedDate, instance.chipText, instance.pasting], [true, "Anthropic API key", "2026-11-20", "Own credential", false]);
});

test("a refused save keeps the form open and the typed value, and says why", async () => {
  const { instance, field } = setup({ gateway: fakeGateway({ refuse: true }) });
  instance.startPaste();
  field.value = "typo";
  await instance.save();
  assert.equal(instance.error.code, Codes.INVALID_INPUT);
  assert.deepEqual([instance.pasting, field.value, instance.isSet], [true, "typo", false]);
});

test("removing asks first, and keeping it changes nothing", async () => {
  const { instance, gateway } = setup({ seed: { project_id: "p1", set: true, kind: "oauth", updated: "2026-11-01T00:00:00Z", global: { set: true, kind: "oauth" } } });
  instance.askRemove();
  assert.equal(instance.confirming, true);
  instance.keep();
  assert.deepEqual([instance.confirming, instance.isSet, gateway.calls], [false, true, []]);
  instance.askRemove();
  await instance.remove();
  await flush();
  assert.deepEqual([instance.isSet, gateway.calls], [false, [["remove", "p1"]]]);
});
