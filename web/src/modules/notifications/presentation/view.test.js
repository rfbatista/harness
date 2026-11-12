import { assert, file, test } from "../../../shared/testing/test.js";
import { makeNotification, N0 } from "../testing/fixtures.js";
import { toRow } from "./view.js";

file("notifications/presentation/view");

const restart = (fields = {}) =>
  makeNotification({
    kind: "sessions_interrupted",
    link: "/notifications",
    interruptedSessions: [
      { sessionId: "s1", projectId: "p1", taskId: "t1", taskTitle: "Ship", agentName: "go-developer", link: "/projects/p1/tasks/t1?session=s1", state: "interrupted", reason: "" },
      { sessionId: "s2", projectId: "p1", taskId: "", taskTitle: "", agentName: "devops", link: "/projects/p1?session=s2", state: "failed", reason: "the worktree is gone" },
    ],
    ...fields,
  });

test("a sessions_interrupted item lists each session with its task, agent, state and the reason a resume failed", () => {
  const row = toRow(restart(), N0);
  assert.deepEqual(row.sessions.map((s) => [s.key, s.label, s.href, s.state, s.reason, s.hasReason]), [
    ["s1", "Ship · go-developer", "/projects/p1/tasks/t1?session=s1", "interrupted", "", false],
    ["s2", "No task · devops", "/projects/p1?session=s2", "failed", "the worktree is gone", true],
  ]);
  assert.deepEqual([row.hasSessions, row.canResumeAll], [true, true]);
});

test("Resume all interrupted is offered while the item is not settled, and other kinds have no list", () => {
  assert.equal(toRow(restart({ resolvedAt: N0 }), N0).canResumeAll, false);
  const plain = toRow(makeNotification(), N0);
  assert.deepEqual([plain.sessions, plain.hasSessions, plain.canResumeAll], [[], false, false]);
});
