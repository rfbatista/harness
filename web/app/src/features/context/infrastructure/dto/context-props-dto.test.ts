import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { ContextPropsDto } from "./context-props-dto";

describe("ContextPropsDto", () => {
  test("reads the session, its project, whether it runs and the group to open", () => {
    const dto = ContextPropsDto.fromJSON({ session_id: "s1", project_id: "p1", live: true, group: "instructions", parent: "architect", extra: 1 });
    assert.deepEqual(dto.toJSON(), { session_id: "s1", project_id: "p1", live: true, group: "instructions", parent: "architect", focus: false });
  });

  test("a missing group is none", () => {
    assert.equal(ContextPropsDto.fromJSON({ session_id: "s1", project_id: "p1", live: false }).group, "");
  });

  test("names the field that is wrong", () => {
    assert.throws(() => ContextPropsDto.fromJSON({ session_id: "s1", project_id: "p1", live: "yes" }), (e) => e instanceof DtoError && e.path === "live");
  });
});
