import { assert, file, test } from "../../../shared/testing/test.js";
import { toTask } from "./dto.js";

file("tasks/infrastructure/dto");

test("a task keeps when it last changed, which orders Done on the rail", () => {
  assert.equal(toTask({ id: "t1", status: "done", updated_at: "2026-10-09T11:00:00Z" }).updatedAt, Date.UTC(2026, 9, 9, 11));
  assert.equal(toTask({ id: "t1", status: "done" }).updatedAt, 0, "unknown: oldest");
});

test("a task reads its parent; none, null or absent is top-level", () => {
  assert.equal(toTask({ id: "c1", status: "todo", parent_id: "f1" }).parentId, "f1");
  assert.equal(toTask({ id: "t1", status: "todo", parent_id: "" }).parentId, "");
  assert.equal(toTask({ id: "t1", status: "todo", parent_id: null }).parentId, "");
  assert.equal(toTask({ id: "t1", status: "todo" }).parentId, "");
});
