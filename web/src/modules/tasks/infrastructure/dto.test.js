import { assert, file, test } from "../../../shared/testing/test.js";
import { toTask } from "./dto.js";

file("tasks/infrastructure/dto");

test("a task keeps when it last changed, which orders Done on the rail", () => {
  assert.equal(toTask({ id: "t1", status: "done", updated_at: "2026-10-09T11:00:00Z" }).updatedAt, Date.UTC(2026, 9, 9, 11));
  assert.equal(toTask({ id: "t1", status: "done" }).updatedAt, 0, "unknown: oldest");
});

test("a task reads the board's fields: when it entered its column, its last move, its merge state and whether it is archived", () => {
  const t = toTask({
    id: "t1",
    status: "review",
    created_at: "2026-10-01T10:00:00Z",
    status_since: "2026-10-19T09:12:00Z",
    archived_at: null,
    last_move: { status: "review", by: "session", session_id: "s-go", session_label: "go-developer", reason: "All tests pass.", at: "2026-10-19T09:12:00Z" },
    merge_state: { state: "partial", merged: 1, total: 2 },
  });
  assert.equal(t.statusSince, Date.UTC(2026, 9, 19, 9, 12));
  assert.equal(t.createdAt, Date.UTC(2026, 9, 1, 10));
  assert.equal(t.archivedAt, 0);
  assert.deepEqual(t.lastMove, { status: "review", by: "session", sessionId: "s-go", sessionLabel: "go-developer", reason: "All tests pass.", at: Date.UTC(2026, 9, 19, 9, 12) });
  assert.deepEqual(t.mergeState, { state: "partial", merged: 1, total: 2 });
  assert.equal(toTask({ id: "t1", status: "done", archived_at: "2026-10-19T12:00:00Z" }).archivedAt, Date.UTC(2026, 9, 19, 12));
});

test("an old event without the board's fields reads them as null, and the time in column falls back to the creation", () => {
  const t = toTask({ id: "t1", status: "todo", created_at: "2026-10-01T10:00:00Z" });
  assert.deepEqual([t.statusSince, t.archivedAt, t.lastMove, t.mergeState], [Date.UTC(2026, 9, 1, 10), 0, null, null]);
  const bare = toTask({ id: "t1", status: "todo" });
  assert.deepEqual([bare.statusSince, bare.createdAt], [0, 0]);
  const goZero = toTask({ id: "t1", status: "todo", created_at: "0001-01-01T00:00:00Z" });
  assert.deepEqual([goZero.statusSince, goZero.createdAt], [0, 0], "the server's zero time is unknown, not year 1");
});

test("a malformed last move or merge state is left out rather than failing the task", () => {
  const t = toTask({ id: "t1", status: "review", last_move: { by: "person" }, merge_state: { merged: 1 } });
  assert.deepEqual([t.lastMove, t.mergeState], [null, null]);
});

test("a task reads its parent; none, null or absent is top-level", () => {
  assert.equal(toTask({ id: "c1", status: "todo", parent_id: "f1" }).parentId, "f1");
  assert.equal(toTask({ id: "t1", status: "todo", parent_id: "" }).parentId, "");
  assert.equal(toTask({ id: "t1", status: "todo", parent_id: null }).parentId, "");
  assert.equal(toTask({ id: "t1", status: "todo" }).parentId, "");
});
