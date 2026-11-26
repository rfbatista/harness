import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { MemoryPropsDto } from "./memory-props-dto";

describe("MemoryPropsDto", () => {
  test("reads the session and whether it is live, and writes them back", () => {
    const props = MemoryPropsDto.fromJSON({ session_id: "s1", live: true, extra: 1 });
    assert.deepEqual(JSON.parse(JSON.stringify(props)), { session_id: "s1", live: true });
  });

  test("refuses props without a session", () => {
    assert.throws(() => MemoryPropsDto.fromJSON({ live: true }), DtoError);
  });
});
