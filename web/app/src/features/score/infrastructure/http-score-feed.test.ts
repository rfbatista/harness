import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { httpScoreFeed, toRunChangeOrNull } from "./http-score-feed";

describe("the project feed's bench run messages", () => {
  test("a bench_run message is a run change; any other kind is none", () => {
    assert.deepEqual(toRunChangeOrNull({ bench_run: { run_id: "r1", status: "running", done: 2, total: 5 } }), { runId: "r1", status: "running", done: 2, total: 5, scorecardId: "" });
    assert.deepEqual(toRunChangeOrNull({ bench_run: { run_id: "r1", status: "finished", done: 5, total: 5, scorecard_id: "r1" } })?.scorecardId, "r1");
    assert.equal(toRunChangeOrNull({ session: {} }), null);
    assert.equal(toRunChangeOrNull({ bench_run: { run_id: "r1", status: "weird" } }), null);
  });

  test("it follows the project's feed and hands over only run changes", () => {
    const feed = memoryFeed();
    const seen: string[] = [];
    const stop = httpScoreFeed(feed).follow("p1", { onRun: (c) => seen.push(`${c.runId}:${c.done}`) });
    feed.emit("/events?project_id=p1", { ticket: {} });
    feed.emit("/events?project_id=p1", { bench_run: { run_id: "r1", status: "running", done: 1, total: 3 } });
    stop();
    feed.emit("/events?project_id=p1", { bench_run: { run_id: "r1", status: "running", done: 2, total: 3 } });
    assert.deepEqual(seen, ["r1:1"]);
  });
});
