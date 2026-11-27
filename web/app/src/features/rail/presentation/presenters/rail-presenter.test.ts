import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Rail } from "../../application/rail";
import type { RailSeed } from "../../application/ports";
import type { RailSession } from "../../domain/activity";
import type { Task } from "../../domain/task";
import { manualScheduler, memoryAnnouncer, memoryFeaturePrefs, memoryPageOpener, memoryRailPrefs } from "../../infrastructure/memory-ports";
import { memoryRailFeed, memoryRailSource, memoryServicesCount } from "../../infrastructure/memory-rail";
import { session, task } from "../../testing/fixtures";
import type { ProjectNav } from "../../domain/nav";
import { RailPresenter } from "./rail-presenter";
import { SERVER_PAINT_ROWS } from "./rail-rows";
import type { RowViewModel } from "./rail-rows";

const NAV: ProjectNav = { newTaskHref: "/projects/p1/tasks/new", boardHref: "/projects/p1", codeHref: "", servicesHref: "/projects/p1/services", servicesProjectId: "p1", reviewsHref: "/projects/p1/reviews", documentsHref: "", designHref: "/projects/p1/design", designSystemHref: "/projects/p1/design-system", designSystemProposals: 2, usageHref: "", agentSystemHref: "/projects/p1/agent-system", scoreHref: "/projects/p1/score", productOwnerHref: "/projects/p1/owner" };

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
  const services = memoryServicesCount();
  const rail = new Rail({ source, feed, services, servicesRunning: 120, seed, currentTaskId: currentTask, collapsed, prefs, features, announcer, opener, scheduler });
  const presenter = new RailPresenter(rail, features, { projectName: "harness", nav: NAV });
  const vm = () => presenter.viewModel;
  const group = (key: string) => vm().groups.find((g) => g.key === key)!;
  const rows = (): RowViewModel[] => vm().groups.flatMap((g) => [...g.rows, ...g.more]);
  const row = (id: string): RowViewModel | undefined => rows().flatMap((r) => [r, ...r.kids]).find((r) => r.id === id);
  return { services, source, feed, prefs, features, announcer, opener, scheduler, presenter, vm, group, row };
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

  test("a feature opened in the browser's preference is folded as the server painted it, and opens once the island has taken over", () => {
    const { row, presenter } = setup({ tasks: [task({ id: "f" }), task({ id: "k1", parentId: "f", status: "todo" })], sessions: [], featuresOpen: ["f"] });
    assert.equal(row("f")!.open, false);
    presenter.hydrated();
    assert.equal(row("f")!.open, true);
  });

  test("a child shows its status word when nothing else applies, and no merge badge", () => {
    const { row, presenter } = setup({ tasks: [task({ id: "f" }), task({ id: "k1", parentId: "f", status: "review", mergeState: { state: "merged", merged: 1, total: 1 } })], sessions: [], featuresOpen: ["f"] });
    presenter.hydrated();
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

  test("the server's paint of a folded group holds only the open task and what needs the person; the island adds the rest", () => {
    const tasks = [task({ id: "b1", status: "backlog" }), task({ id: "b2", status: "backlog" }), task({ id: "b3", status: "backlog" })];
    const { group, presenter } = setup({ tasks, sessions: [session("s", "b2", "idle")], currentTask: "b3", collapsed: ["backlog"] });
    assert.deepEqual([group("backlog").rows.map((r) => r.id), group("backlog").countText], [["b2", "b3"], "3"]);
    assert.equal(group("backlog").countWords, " tasks, 1 needs you");
    presenter.hydrated();
    assert.deepEqual(group("backlog").rows.map((r) => r.id), ["b1", "b2", "b3"]);
  });

  test("the server's paint is a first screenful of rows, always with the open task; the island adds the rest", () => {
    const tasks = Array.from({ length: 60 }, (_, n) => task({ id: `t${n}`, title: `Task ${n}`, status: n < 40 ? "todo" : "backlog" }));
    const { vm, group, presenter } = setup({ tasks, sessions: [session("s", "t5", "idle"), session("w", "t55", "idle")], currentTask: "t45" });
    const painted = vm().groups.reduce((n, g) => n + g.rows.length, 0);
    assert.equal(painted, SERVER_PAINT_ROWS + 1, "the budget, plus the open task, which is always drawn");
    assert.deepEqual(group("backlog").rows.map((r) => r.id), ["t45"], "the budget went to Todo; a waiting task beyond it arrives with the island");
    assert.equal(group("todo").rows.length, SERVER_PAINT_ROWS);
    assert.equal(group("backlog").countText, "20", "the counts are the project's, not the paint's");
    assert.equal(group("backlog").countWords, " tasks, 1 needs you", "and so are the words");
    presenter.hydrated();
    assert.deepEqual([group("todo").rows.length, group("backlog").rows.length], [40, 20]);
  });

  test("Done caps at five with Show N more, and the open task stays in view", () => {
    const tasks = Array.from({ length: 8 }, (_, n) => task({ id: `d${n}`, title: `Done ${n}`, status: "done", updatedAt: n }));
    const { group, presenter } = setup({ tasks, sessions: [], currentTask: "d0" });
    assert.deepEqual([group("done").rows.map((r) => r.id), group("done").moreText, group("done").hasMore], [["d7", "d6", "d5", "d4", "d3", "d0"], "Show 2 more", true]);
    assert.deepEqual(group("done").more, [], "the server's paint leaves the rest out");
    presenter.hydrated();
    assert.deepEqual(group("done").more.map((r) => r.id), ["d2", "d1"]);
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

  test("the row with focus peeks out of a folded group until focus leaves", () => {
    const { presenter, row } = setup();
    presenter.setFocused("t1");
    assert.equal(row("t1")!.peek, true);
    presenter.clearFocusedRow();
    assert.equal(row("t1")!.peek, false);
  });
});

describe("the project navigation", () => {
  test("it lists the pages the server serves, in order, the open one current, with badges", () => {
    const { presenter, vm } = setup({ tasks: [task({ pendingReviews: 3 })] });
    assert.deepEqual(vm().nav.map((i) => i.label), ["Board", "Services", "Review requests", "Design assets", "Design systems", "Agent system", "Score", "Product owner"]);
    presenter.setPage("board");
    assert.deepEqual(vm().nav.filter((i) => i.current).map((i) => i.key), ["board"]);
    const byKey = Object.fromEntries(vm().nav.map((i) => [i.key, i]));
    assert.deepEqual([byKey["services"]?.badge, byKey["reviews"]?.badge, byKey["design-system"]?.proposals, byKey["board"]?.badge], ["99+", "3", 2, ""]);
    presenter.setPage("");
    assert.equal(vm().nav.some((i) => i.current), false);
    assert.equal(vm().newTaskHref, "/projects/p1/tasks/new");
  });

  test("Product owner is the last row, after Usage, and current on its page; a page the server does not serve has no row", () => {
    const { presenter, vm } = setup();
    assert.equal(vm().nav.at(-1)?.label, "Product owner");
    presenter.setPage("product-owner");
    assert.deepEqual(vm().nav.filter((i) => i.current).map((i) => i.key), ["product-owner"]);
    assert.equal(vm().nav.some((i) => i.label === "Usage"), false, "usageHref is empty in this project");
  });

  test("Agent system sits after Usage and before Score, and is current on its page", () => {
    const { presenter, vm } = setup();
    const labels = vm().nav.map((i) => i.label);
    assert.equal(labels.indexOf("Agent system"), labels.indexOf("Score") - 1);
    presenter.setPage("agent-system");
    assert.deepEqual(vm().nav.filter((i) => i.current).map((i) => i.key), ["agent-system"]);
  });

  test("Score sits after Agent system and before Product owner, and is current on its page", () => {
    const { presenter, vm } = setup();
    const labels = vm().nav.map((i) => i.label);
    assert.equal(labels.indexOf("Score"), labels.indexOf("Product owner") - 1);
    presenter.setPage("score");
    assert.deepEqual(vm().nav.filter((i) => i.current).map((i) => i.key), ["score"]);
  });

  test("the Review requests badge follows the feed", () => {
    const { feed, vm } = setup({ tasks: [task()] });
    assert.equal(vm().nav.find((i) => i.key === "reviews")?.badge, "");
    feed.emit({ kind: "task-upsert", task: task({ pendingReviews: 2 }) });
    assert.equal(vm().nav.find((i) => i.key === "reviews")?.badge, "2");
  });
});

describe("the Services badge", () => {
  const badge = (vm: () => { nav: readonly { key: string; badge: string; running: number | null }[] }) => vm().nav.find((i) => i.key === "services");

  test("it starts from what the server counted and follows the services feed, ignoring an event it has seen", () => {
    const { services, vm } = setup();
    assert.deepEqual([badge(vm)?.badge, badge(vm)?.running], ["99+", 120]);
    services.emit({ seq: 5, running: 2 });
    assert.deepEqual([badge(vm)?.badge, badge(vm)?.running], ["2", 2]);
    services.emit({ seq: 5, running: 7 });
    services.emit({ seq: 4, running: 9 });
    assert.equal(badge(vm)?.running, 2, "an old or repeated sequence number is not news");
    services.emit({ seq: 6, running: 0 });
    assert.equal(badge(vm)?.badge, "", "the badge is empty at zero");
  });

  test("after a resync it reads the list again and takes its sequence number", async () => {
    const { services, vm } = setup();
    services.emit({ seq: 5, running: 2 });
    services.answer({ seq: 9, running: 4 });
    services.setStatus("resynced");
    await flush();
    assert.equal(badge(vm)?.running, 4);
    services.emit({ seq: 8, running: 1 });
    assert.equal(badge(vm)?.running, 4, "the list was current to 9");
    services.answer(null);
    services.setStatus("resynced");
    await flush();
    assert.equal(badge(vm)?.running, 4, "a list that cannot be read keeps the count");
  });

  test("stopping stops following the services feed", () => {
    const { services, presenter } = setup();
    presenter.stop();
    assert.equal(services.followers(), 0);
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
