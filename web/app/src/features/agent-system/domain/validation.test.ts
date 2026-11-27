import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { check } from "./validation";

describe("row validation", () => {
  test("a reset (null) is always allowed", () => {
    assert.equal(check("guard.retry_limit", { kind: "int", min: 1, max: 10 }, null), undefined);
  });
  test("a number outside its range is OUT_OF_RANGE with the range; a string is INVALID_TYPE", () => {
    const rule = { kind: "int", min: 1, max: 10 } as const;
    assert.deepEqual(check("guard.retry_limit", rule, 11), { field: "guard.retry_limit", code: "OUT_OF_RANGE", message: "Takes 1–10." });
    assert.equal(check("guard.retry_limit", rule, "3")?.code, "INVALID_TYPE");
    assert.equal(check("guard.retry_limit", rule, 2.5)?.code, "INVALID_TYPE");
    assert.equal(check("guard.retry_limit", rule, 3), undefined);
  });
  test("an emptied required command is refused with the way back", () => {
    const error = check("guard.gates.test.command", { kind: "required-command" }, "  ");
    assert.equal(error?.code, "REQUIRED_GATE_MISSING");
    assert.match(error?.message ?? "", /Reset the field to inherit/);
    assert.equal(check("guard.gates.test.command", { kind: "required-command" }, "go test ./..."), undefined);
  });
  test("a list takes only non-empty text entries", () => {
    assert.equal(check("guard.protected_paths", { kind: "list" }, ["a/**", ""])?.code, "INVALID_TYPE");
    assert.equal(check("guard.protected_paths", { kind: "list" }, ["a/**"]), undefined);
  });
  test("a switch takes a boolean", () => {
    assert.equal(check("learn.context_map.enabled", { kind: "bool" }, "yes")?.code, "INVALID_TYPE");
  });
});
