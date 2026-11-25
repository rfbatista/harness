import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Rail } from "../../application/rail";
import type { RailFeedStatus, RailSeed } from "../../application/ports";
import type { RailSession } from "../../domain/activity";
import type { Task } from "../../domain/task";
import { manualScheduler, memoryAnnouncer, memoryFeaturePrefs, memoryPageOpener, memoryRailPrefs } from "../../infrastructure/memory-ports";
import { memoryRailFeed, memoryRailSource } from "../../infrastructure/memory-rail";
import { session, task } from "../../testing/fixtures";
import { RailPresenter } from "./rail-presenter";
import type { RowViewModel } from "./rail-rows";

const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

interface Options {
  tasks?: Task[];
  sessions?: RailSession[];
  currentTask?: string;
  collapsed?: string[];
  featuresOpen?: string[];
  archived?: Task[];
}

function setup({ tasks = [task()], sessions = [session("a", "t1", "running")], currentTask = "t1", collapsed = [], featuresOpen = [], archived = [] }: Options = {}) {
  const seed: RailSeed = { projectId: "p1", tasks, sessions, archivedCount: archived.length };
  const source = memoryRailSource({ tasks, sessions, archived });
  const feed = memoryRailFeed();
  const prefs = memoryRailPrefs(collapsed);
  const features = memoryFeaturePrefs(featuresOpen);
  const announcer = memoryAnnouncer();
  const opener = memoryPageOpener();
  const scheduler = manualScheduler();
  const statuses: RailFeedStatus[] = [];
  const rail = new Rail({ source, feed, seed, currentTaskId: currentTask, collapsed, prefs, features, announcer, opener, scheduler, onFeedStatus: (s) => statuses.push(s) });
  const presenter = new RailPresenter(rail, features);
  const vm = () => presenter.viewModel;
  const group = (key: string) => vm().groups.find((g) => g.key === key)!;
  const rows = (): RowViewModel[] => vm().groups.flatMap((g) => [...g.rows, ...g.more]);
  const row = (id: string): RowViewModel | undefined => rows().flatMap((r) => [r, ...r.kids]).find((r) => r.id === id);
  return { source, feed, prefs, features, announcer, opener, scheduler, statuses, presenter, vm, group, row };
}

describe("the rail's groups and rows", () => {
  test("every group comes from the model in the rail's order, the open task marked current", () => {
    const { vm, row } = setup({ tasks: [task({ id: "t-done", title: "Ship it", status: "done" }), task(), task({ id: "t2", title: "Write docs", status: "todo" })] });
    assert.deepEqual(vm().groups.map((g) => `${g.label} ${g.countText}: ${g.rows.map((r) => r.title).join(", ")}`), ["In progress 1: Add SSE feed", "Review 0: ", "Todo 1: Write docs", "Backlog 0: ", "Done 1: Ship it"]);
    const feed = row("t1")!;
    assert.deepEqual([feed.href, feed.current, feed.state, feed.isLive, feed.liveText], ["/projects/p1/tasks/t1", true, "running", true, "1 live"]);
    assert.deepEqual([row("t2")!.current, row("t2")!.hasDot], [false, false]);
    assert.equal(vm().total, 3);
  });

  test("a ticket change regroups; the last task gone leaves it empty", () => {
    const { feed, vm, group } = setup();
    assert.equal(vm().isEmpty, false);
    feed.emit({ kind: "task-upsert", task: task({ status: "review" }) });
    assert.deepEqual([group("in_progress").countText, group("review").rows.map((r) => r.id)], ["0", ["t1"]]);
    feed.emit({ kind: "task-deleted", id: "t1" });
    assert.deepEqual([vm().isEmpty, vm().hasTasks], [true, false]);
  });

  test("a session started on a task shows on its row as it starts", () => {
    const { feed, row } = setup({ tasks: [task(), task({ id: "t2", title: "Write docs", status: "todo" })] });
    assert.deepEqual([row("t2")!.hasDot, row("t2")!.isLive], [false, false]);
    feed.emit({ kind: "upsert", session: session("b", "t2", "running") });
    assert.deepEqual([row("t2")!.liveText, row("t2")!.state], ["1 live", "running"]);
    feed.emit({ kind: "upsert", session: session("b", "t2", "idle") });
    assert.deepEqual([row("t2")!.state, row("t2")!.isWaiting, row("t2")!.attention], ["waiting", true, true]);
    feed.emit({ kind: "deleted", id: "b" });
    assert.equal(row("t2")!.hasDot, false);
  });

  test("the meta is the first that applies, and the hidden words make the link read like a sentence", () => {
    const { row } = setup({
      tasks: [task({ id: "r", title: "Reviewed", pendingReviews: 2 }), task({ id: "w", title: "Waits" }), task({ id: "l", title: "Runs" }), task({ id: "m", title: "Merged", status: "review", mergeState: { state: "partial", merged: 1, total: 2 } })],
      sessions: [session("1", "w", "idle"), session("2", "l", "running"), session("3", "l", "running")],
    });
    assert.deepEqual([row("r")!.reviewsText, row("r")!.lead, row("r")!.tail], ["2 reviews", ", ", " waiting on you"]);
    assert.deepEqual([row("w")!.isWaiting, row("w")!.lead, row("w")!.tail], [true, ", ", " on you"]);
    assert.deepEqual([row("l")!.liveText, row("l")!.lead, row("l")!.tail], ["2 live", ", running, ", " sessions"]);
    assert.deepEqual([row("m")!.hasMerge, row("m")!.mergeState, row("m")!.mergeText], [true, "partial", "1/2 merged"]);
  });

  test("a feature row nests its children, folded unless opened, and covers them while folded", () => {
    const tasks = [task({ id: "f", title: "Files tab" }), task({ id: "k1", parentId: "f", title: "Notes pane", status: "todo", pendingReviews: 1 })];
    const { row, presenter, features, vm } = setup({ tasks, sessions: [] });
    const f = row("f")!;
    assert.deepEqual([f.isFeature, f.open, f.kidsCount, f.twistyLabel, f.hasReviews, f.reviewsText], [true, false, 1, "Expand Files tab, 1 task", true, "1 review"]);
    presenter.toggleFeature("f");
    assert.deepEqual([row("f")!.open, features.isOpen("f"), row("f")!.twistyLabel, row("f")!.hasReviews], [true, true, "Collapse Files tab, 1 task", false]);
    const kid = row("k1")!;
    assert.deepEqual([kid.isKid, kid.hasReviews, vm().groups[0]!.rows.length], [true, true, 1]);
  });

  test("a feature opened in the browser's preference starts open", () => {
    const { row } = setup({ tasks: [task({ id: "f" }), task({ id: "k1", parentId: "f", status: "todo" })], sessions: [], featuresOpen: ["f"] });
    assert.equal(row("f")!.open, true);
  });

  test("a child shows its status word when nothing else applies, and no merge badge", () => {
    const { row } = setup({ tasks: [task({ id: "f" }), task({ id: "k1", parentId: "f", status: "review", mergeState: { state: "merged", merged: 1, total: 1 } })], sessions: [], featuresOpen: ["f"] });
    const kid = row("k1")!;
    assert.deepEqual([kid.hasMerge, kid.hasStatusWord, kid.statusWord, kid.statusKey], [false, true, "Review", "review"]);
  });

  test("Review requests counts what waits on the person across the project, live", () => {
    const { feed, vm, row } = setup({ tasks: [task({ id: "t1", pendingReviews: 2 }), task({ id: "t2", status: "todo" })], sessions: [] });
    assert.equal(vm().reviewsCount, 2);
    assert.deepEqual([row("t1")!.state, row("t1")!.reviewsText], ["waiting", "2 reviews"]);
    feed.emit({ kind: "task-upsert", task: task({ id: "t1", pendingReviews: 0 }) });
    assert.deepEqual([vm().reviewsCount, row("t1")!.hasDot], [0, false]);
  });
});

describe("folding", () => {
  test("folding a group is remembered, and the rail starts folded as the server painted it", () => {
    const { presenter, prefs, group, vm } = setup({ tasks: [task({ id: "b1", status: "backlog" }), task({ id: "d1", status: "done" })], collapsed: ["done", "review"] });
    assert.deepEqual([group("done").collapsed, group("done").expanded, group("review").collapsed], [true, false, false]);
    presenter.toggleGroup("backlog");
    assert.deepEqual([group("backlog").collapsed, prefs.collapsed()], [true, ["done", "review", "backlog"]]);
    presenter.toggleGroup("done");
    assert.deepEqual([group("done").collapsed, prefs.collapsed()], [false, ["review", "backlog"]]);
    assert.equal(vm().turned, true, "the chevron turns once a click folded something");
  });

  test("Done caps at five with Show N more, and the open task stays in view", () => {
    const tasks = Array.from({ length: 8 }, (_, n) => task({ id: `d${n}`, title: `Done ${n}`, status: "done", updatedAt: n }));
    const { group } = setup({ tasks, sessions: [], currentTask: "d0" });
    assert.deepEqual([group("done").rows.map((r) => r.id), group("done").more.map((r) => r.id), group("done").moreText], [["d7", "d6", "d5", "d4", "d3", "d0"], ["d2", "d1"], "Show 2 more"]);
  });
});

describe("the filter and Needs you", () => {
  test("the filter keeps matching tasks, says how many 500ms after typing stops, and Esc clears it", () => {
    const { presenter, scheduler, group, vm } = setup({ tasks: [task({ id: "t1", title: "Redesign sidebar" }), task({ id: "t2", title: "Write docs", status: "todo" })] });
    presenter.setQuery("side");
    assert.deepEqual([group("in_progress").countText, group("todo").countText, group("todo").foldable], ["1 of 1", "0 of 1", false]);
    assert.deepEqual(scheduler.pending(), [500]);
    scheduler.advance(500);
    assert.equal(vm().filterResult, "1 task matches");
    presenter.clearQuery();
    assert.deepEqual([vm().query, vm().filterResult, group("todo").countText], ["", "", "1"]);
  });

  test("typing again restarts the wait; only the last text is read out", () => {
    const { presenter, scheduler, vm } = setup({ tasks: [task({ id: "t1", title: "Redesign sidebar" })] });
    presenter.setQuery("s");
    scheduler.advance(300);
    presenter.setQuery("side");
    scheduler.advance(300);
    assert.equal(vm().filterResult, "");
    scheduler.advance(200);
    assert.equal(vm().filterResult, "1 task matches");
  });

  test("the match is highlighted in the title", () => {
    const { presenter, row } = setup({ tasks: [task({ id: "t1", title: "Redesign sidebar" })] });
    presenter.setQuery("SIDE");
    assert.deepEqual([row("t1")!.before, row("t1")!.match, row("t1")!.after, row("t1")!.hasMatch], ["Redesign ", "side", "bar", true]);
  });

  test("no match at all says so; Enter opens the first match", () => {
    const { presenter, vm, opener, scheduler } = setup({ tasks: [task({ id: "t1", title: "Redesign sidebar" }), task({ id: "t2", title: "Sidebar keys", status: "todo" })] });
    presenter.setQuery("zzz");
    scheduler.advance(500);
    assert.deepEqual([vm().hasNoMatch, vm().noMatchText, vm().filterResult], [true, "No task matches “zzz”.", "No task matches"]);
    presenter.setQuery("sidebar");
    assert.equal(presenter.openFirstMatch(), true);
    assert.deepEqual(opener.opened, ["/projects/p1/tasks/t1"]);
    presenter.setQuery("");
    assert.equal(presenter.openFirstMatch(), false, "Enter with no text opens nothing");
    assert.equal(opener.opened.length, 1);
  });

  test("Needs you shows only what waits on the person, and stays on when nothing is left", () => {
    const { presenter, feed, vm, group } = setup({ tasks: [task({ id: "t1" }), task({ id: "t2", status: "todo", pendingReviews: 1 })], sessions: [] });
    assert.deepEqual([vm().needsYouCount, vm().showsNeedsYou, vm().needsYouOnly], [1, true, false]);
    presenter.toggleNeedsYou();
    assert.deepEqual([group("in_progress").countText, group("todo").rows.map((r) => r.id)], ["0 of 1", ["t2"]]);
    feed.emit({ kind: "task-upsert", task: task({ id: "t2", status: "todo", pendingReviews: 0 }) });
    assert.deepEqual([vm().needsYouCount, vm().needsYouOnly, vm().showsNeedsYou, vm().nothingNeedsYou], [0, true, true, true]);
    presenter.toggleNeedsYou();
    assert.deepEqual([vm().showsNeedsYou, vm().nothingNeedsYou], [false, false]);
  });

  test("the toggle is hidden at zero unless it is on", () => {
    const { vm } = setup({ sessions: [] });
    assert.deepEqual([vm().needsYouCount, vm().showsNeedsYou], [0, false]);
  });
});

describe("live changes", () => {
  test("a task that starts needing the person rings its dot once and is announced, several at once in one sentence", () => {
    const { feed, row, scheduler, announcer } = setup({ tasks: [task({ id: "t1", title: "Fix flaky test" }), task({ id: "t2", title: "Write docs", status: "todo" }), task({ id: "t3", title: "Port", status: "todo" })], sessions: [] });
    feed.emit({ kind: "upsert", session: session("a", "t1", "idle") });
    assert.equal(row("t1")!.signal, true);
    scheduler.advance(2000);
    assert.deepEqual(announcer.said, ["“Fix flaky test” is waiting on you."]);
    assert.equal(row("t1")!.signal, false, "the ring is one-shot");
    feed.emit({ kind: "upsert", session: session("b", "t2", "waiting_approval") });
    feed.emit({ kind: "task-upsert", task: task({ id: "t3", title: "Port", status: "todo", pendingReviews: 1 }) });
    scheduler.advance(2000);
    assert.equal(announcer.said.at(-1), "2 tasks are waiting on you.");
    feed.emit({ kind: "deleted", id: "a" });
    scheduler.advance(2000);
    assert.equal(announcer.said.length, 2, "nothing is said when a task stops waiting");
  });

  test("a task that arrives or moves takes the arrival wash; a rename does not", () => {
    const { feed, row, scheduler } = setup();
    feed.emit({ kind: "task-upsert", task: task({ id: "t2", title: "New", status: "todo" }) });
    feed.emit({ kind: "task-upsert", task: task({ status: "review" }) });
    assert.deepEqual([row("t2")!.fresh, row("t1")!.fresh], [true, true]);
    scheduler.advance(8000);
    feed.emit({ kind: "task-upsert", task: task({ status: "review", title: "Renamed" }) });
    assert.deepEqual([row("t2")!.fresh, row("t1")!.fresh], [false, false]);
  });

  test("after a resync it reads the lists again, quietly: no wash, no announcement", async () => {
    const { feed, source, row, scheduler, announcer } = setup();
    source.setSessions([session("a", "t1", "done"), session("c", "t9", "idle")]);
    source.setTasks([task({ id: "t9", title: "Elsewhere", status: "done" })]);
    feed.setStatus("resynced");
    await flush();
    assert.deepEqual([row("t9")!.liveText, row("t9")!.fresh, row("t9")!.signal], ["1 live", false, false]);
    scheduler.advance(10_000);
    assert.deepEqual(announcer.said, []);
  });

  test("the feed's status reaches whoever reports it to the stream bar", () => {
    const { feed, statuses } = setup();
    feed.setStatus("paused");
    assert.deepEqual(statuses, ["connecting", "paused"]);
  });

  test("the row with focus peeks out of a folded group until focus leaves", () => {
    const { presenter, row } = setup();
    presenter.setFocused("t1");
    assert.equal(row("t1")!.peek, true);
    presenter.clearFocusedRow();
    assert.equal(row("t1")!.peek, false);
  });
});

describe("navigation and the drawer", () => {
  test("in-place navigation moves the open task's mark", () => {
    const { presenter, row } = setup({ tasks: [task(), task({ id: "t2", status: "todo" })] });
    presenter.setCurrentTask("t2");
    assert.deepEqual([row("t1")!.current, row("t2")!.current], [false, true]);
    presenter.setCurrentTask("");
    assert.deepEqual([row("t1")!.current, row("t2")!.current], [false, false]);
  });

  test("the drawer opens and closes", () => {
    const { presenter, vm } = setup();
    presenter.openDrawer();
    assert.equal(vm().drawerOpen, true);
    presenter.closeDrawer();
    assert.equal(vm().drawerOpen, false);
  });

  test("stopping unfollows the feed and drops the timers", () => {
    const { presenter, feed, scheduler } = setup();
    presenter.setQuery("x");
    presenter.stop();
    assert.equal(feed.followers(), 0);
    assert.deepEqual(scheduler.pending(), []);
  });
});
