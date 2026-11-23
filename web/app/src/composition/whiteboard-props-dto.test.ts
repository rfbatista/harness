import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../shared/infrastructure/dto/dto-error";
import { WhiteboardPropsDto } from "./whiteboard-props-dto";

describe("WhiteboardPropsDto", () => {
  test("reads the task, its title and the agent names, and ignores what else the page seed holds", () => {
    const dto = WhiteboardPropsDto.fromJSON({ project_id: "p1", ticket_id: "t1", task_title: "Ship", agent_names: { a1: "dev", bad: 3 }, sessions: [{}], extra: 1 });
    assert.deepEqual(dto.toJSON(), { project_id: "p1", ticket_id: "t1", task_title: "Ship", agent_names: { a1: "dev" } });
  });

  test("a task is required, and the optional parts default to empty", () => {
    assert.throws(() => WhiteboardPropsDto.fromJSON({ project_id: "p1" }), DtoError);
    const dto = WhiteboardPropsDto.fromJSON({ project_id: "p1", ticket_id: "t1" });
    assert.deepEqual(dto.agent_names, {});
  });
});
