import { assert, file, test } from "../../../shared/testing/test.js";
import views from "../../../../testdata/views/project-summary.json" with { type: "json" };
import { summaryView, totalLine } from "./view.js";

file("projects/presentation/view");

const now = new Date("2026-10-07T12:00:00Z");

test("a summary reads as the BFF's first paint does", () => {
  for (const c of views.cases) {
    const got = summaryView(
      {
        project: { id: "p1", name: "x", rootDir: "/x", ignoredPaths: [] },
        repositoryCount: c.repository_count,
        openTaskCount: c.open_task_count,
        runningSessionCount: c.running_session_count,
        lastActivityAt: c.minutes_ago === null ? null : new Date(now.getTime() - c.minutes_ago * 60_000),
      },
      now,
    );
    assert.deepEqual(got, { state: c.state, word: c.word, meta: c.meta });
  }
});

test("the total line counts projects and running sessions", () => {
  for (const c of views.totals) assert.equal(totalLine(c.projects, c.running), c.line);
});
