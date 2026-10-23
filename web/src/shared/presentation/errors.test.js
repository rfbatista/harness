import { Codes, StructuredError } from "../domain/errors.js";
import { assert, file, test } from "../testing/test.js";
import { describeError } from "./errors.js";

file("shared/presentation/errors");

test("a resume refusal says what to do next", () => {
  for (const code of [Codes.SESSION_NOT_INTERACTIVE, Codes.SESSION_ALREADY_RUNNING, Codes.WORKSPACE_MISSING, Codes.SESSION_TRANSCRIPT_MISSING]) {
    const view = describeError(new StructuredError(code, "refused", 409));
    assert.equal(view.code, code);
    assert.ok(view.next !== "retry, or check the server log", `${code} has its own next step`);
  }
});

test("an unknown code falls back to the server log", () => {
  assert.equal(describeError(new Error("boom")).next, "retry, or check the server log");
});

test("project management refusals say what to do next", () => {
  for (const code of [Codes.PROJECT_NAME_TAKEN, Codes.PROJECT_ROOT_INVALID, Codes.PROJECT_HAS_RUNNING_SESSIONS]) {
    const view = describeError(new StructuredError(code, "refused", 409));
    assert.equal(view.code, code);
    assert.ok(view.next !== "retry, or check the server log", `${code} has its own next step`);
  }
});

test("a refused agent start links to the sections of the Agent isolation page that fix it", () => {
  const one = describeError(new StructuredError(Codes.AGENT_CREDENTIAL_MISSING, "no credential", 503));
  assert.deepEqual(one.links.map((l) => l.href), ["/agent-isolation#credential"]);
  assert.ok(one.next !== "retry, or check the server log");

  const details = { missing: [{ code: Codes.MANAGED_CLAUDE_INSTALLING }, { code: Codes.SANDBOX_DRIVER_MISSING }] };
  const several = describeError(new StructuredError(Codes.MANAGED_CLAUDE_INSTALLING, "installing", 503, details));
  assert.deepEqual(several.links.map((l) => l.href), ["/agent-isolation#claude", "/agent-isolation#sandbox"]);

  assert.deepEqual(describeError(new StructuredError(Codes.INVALID_INPUT, "x", 400)).links, []);
});
