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
