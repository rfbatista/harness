import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { toColumns } from "./boardView.js";

file("tasks/presentation/boardView");

test("columns in board order, cards with the rail's dot and count, and their links", () => {
  const tasks = [makeTask({ id: "t1" }), makeTask({ id: "t 2", title: "Write <docs>", status: "todo" })];
  const cols = toColumns(tasks, { projectId: "p/1", byTask: { t1: { live: 2, attention: true } }, fresh: new Set(["t 2"]) });
  assert.deepEqual(cols.map((c) => `${c.status}:${c.label}:${c.count}:${c.isEmpty}`), ["backlog:Backlog:0:true", "todo:Todo:1:false", "in_progress:In progress:1:false", "review:Review:0:true", "done:Done:0:true"]);
  const [feed] = cols[2].cards;
  assert.deepEqual([feed.href, feed.state, feed.word, feed.hasDot, feed.live, feed.hasCount, feed.fresh], ["/projects/p%2F1/tasks/t1", "waiting", "waiting on you", true, 2, true, false]);
  const [docs] = cols[1].cards;
  assert.deepEqual([docs.href, docs.hasDot, docs.hasCount, docs.fresh, docs.moveLabel], ["/projects/p%2F1/tasks/t%202", false, false, true, "Move Write <docs> to"]);
});
