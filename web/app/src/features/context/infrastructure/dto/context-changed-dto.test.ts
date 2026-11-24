import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { ContextChangedDto } from "./context-changed-dto";

const failsAt = (path: string) => (err: unknown) => err instanceof DtoError && err.dto === "ContextChangedDto" && err.path === path;

describe("ContextChangedDto", () => {
  test("reads the contract's message and round-trips", () => {
    const wire = { kind: "context.changed", session_id: "s1", reason: "mcp_status" };
    assert.deepEqual(JSON.parse(JSON.stringify(ContextChangedDto.fromJSON(wire))), wire);
  });

  test("is recognised by its kind: another kind, or none, is not this message", () => {
    assert.throws(() => ContextChangedDto.fromJSON({ kind: "task.updated", session_id: "s1", reason: "usage" }), failsAt("kind"));
    assert.throws(() => ContextChangedDto.fromJSON({ session_id: "s1", reason: "usage" }), failsAt("kind"));
  });

  test("rejects a reason the contract does not list and a missing session", () => {
    assert.throws(() => ContextChangedDto.fromJSON({ kind: "context.changed", session_id: "s1", reason: "new" }), failsAt("reason"));
    assert.throws(() => ContextChangedDto.fromJSON({ kind: "context.changed", reason: "usage" }), failsAt("session_id"));
  });

  test("rejects data that is not an object", () => {
    for (const raw of ["context.changed", null, 7, ["context.changed"]]) assert.throws(() => ContextChangedDto.fromJSON(raw), (e) => e instanceof DtoError);
  });

  test("accepts each reason the contract lists, and ignores extra fields", () => {
    for (const reason of ["entry_added", "usage", "mcp_status"]) assert.equal(ContextChangedDto.fromJSON({ kind: "context.changed", session_id: "s", reason, project_id: "p" }).reason, reason);
  });
});
