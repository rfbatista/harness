import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { task } from "../testing/fixtures";
import type { Task } from "./task";
import { DONE_CAP, matchParts, needsYou, normalize, railGroups, railMeta } from "./rail-groups";

const QUIET = { live: 0, attention: false };
const view = (groups: ReturnType<typeof railGroups>): string[] => groups.map((g) => `${g.key} ${g.matched}/${g.total}${g.collapsed ? " collapsed" : ""}: ${g.rows.map((t) => t.id).join(" ")}${g.more.length ? ` | ${g.more.map((t) => t.id).join(" ")}` : ""}`);
const done = (n: number, over: Partial<Task> = {}): Task => task({ id: `d${n}`, title: `Done ${n}`, status: "done", updatedAt: Date.UTC(2026, 9, 1, n), ...over });

describe("railGroups", () => {
  test("every status has its group, in the rail's order, sentence case, empty or not", () => {
    const groups = railGroups([task({ id: "t1", status: "todo" })]);
    assert.deepEqual(groups.map((g) => `${g.key}:${g.label}:${g.total}:${g.empty}`), ["in_progress:In progress:0:true", "review:Review:0:true", "todo:Todo:1:false", "backlog:Backlog:0:true", "done:Done:0:true"]);
  });

  test("Done shows its five latest, then the open task and what needs the person; the rest are more", () => {
    const tasks = Array.from({ length: 9 }, (_, n) => done(n)).map((t) => (t.id === "d2" ? { ...t, pendingReviews: 1 } : t));
    assert.equal(DONE_CAP, 5);
    assert.equal(view(railGroups(tasks, { currentId: "d0" })).at(-1), "done 9/9: d8 d7 d6 d5 d4 d2 d0 | d3 d1");
  });

  test("a session waiting keeps an old done task shown too", () => {
    const tasks = Array.from({ length: 7 }, (_, n) => done(n));
    assert.equal(view(railGroups(tasks, { byTask: { d1: { live: 1, attention: true } } })).at(-1), "done 7/7: d6 d5 d4 d3 d2 d1 | d0");
  });

  test("the active groups keep the list's order, in full", () => {
    const tasks = ["b3", "b1", "b2"].map((id) => task({ id, status: "backlog", updatedAt: Number(id[1]) }));
    assert.equal(view(railGroups(tasks))[3], "backlog 3/3: b3 b1 b2");
  });

  test("a collapsed group is marked so, unless it is empty; its rows stay for the peek rule", () => {
    const groups = railGroups([task({ id: "b1", status: "backlog" })], { collapsed: ["backlog", "review"] });
    assert.deepEqual([groups[3]?.collapsed, groups[3]?.rows.length, groups[1]?.collapsed], [true, 1, false]);
  });

  test("the filter keeps matching titles anywhere, ignoring case and accents, and sets the cap and folding aside", () => {
    const tasks = [
      task({ id: "t1", title: "Redesign sidebar", status: "in_progress" }),
      task({ id: "t2", title: "Café DESIGN view", status: "backlog" }),
      task({ id: "t3", title: "Write docs", status: "todo" }),
      ...Array.from({ length: 7 }, (_, n) => done(n, { title: n === 0 ? "Old design" : `Done ${n}` })),
    ];
    const groups = railGroups(tasks, { query: "  dÉsign ", collapsed: ["backlog"] });
    assert.deepEqual(view(groups), ["in_progress 1/1: t1", "review 0/0: ", "todo 0/1: ", "backlog 1/1: t2", "done 1/7: d0"]);
    assert.ok(groups.every((g) => g.filtering));
  });

  test("Needs you keeps only what waits on the person, and combines with the filter", () => {
    const tasks = [task({ id: "t1", title: "Redesign sidebar", status: "in_progress" }), task({ id: "t2", title: "Review the UX", status: "todo", pendingReviews: 2 }), task({ id: "t3", title: "Fix flaky test", status: "backlog" })];
    const byTask = { t1: { live: 2, attention: false }, t3: { live: 1, attention: true } };
    assert.deepEqual(view(railGroups(tasks, { byTask, needsYouOnly: true })), ["in_progress 0/1: ", "review 0/0: ", "todo 1/1: t2", "backlog 1/1: t3", "done 0/0: "]);
    assert.equal(view(railGroups(tasks, { byTask, needsYouOnly: true, query: "flaky" }))[3], "backlog 1/1: t3");
    assert.equal(railGroups(tasks, { byTask, needsYouOnly: true, query: "flaky" })[2]?.matched, 0);
  });

  test("a feature task nests its children, in review-first order; an archived child and a child with its own row are not drawn twice", () => {
    const tasks = [
      task({ id: "f", title: "Files tab", status: "in_progress" }),
      task({ id: "k1", parentId: "f", status: "done", statusSince: 1 }),
      task({ id: "k2", parentId: "f", status: "review", statusSince: 1 }),
      task({ id: "k3", parentId: "f", status: "todo", archivedAt: 9 }),
    ];
    const [inProgress, review, , , doneGroup] = railGroups(tasks);
    assert.deepEqual(inProgress?.rows.map((t) => t.id), ["f"]);
    assert.deepEqual(inProgress?.kids["f"]?.map((t) => t.id), ["k2", "k1"]);
    assert.equal(inProgress?.kidTotals["f"], 2);
    assert.deepEqual([review?.rows.length, doneGroup?.rows.length], [0, 0], "children have no group row of their own");
  });
});

describe("needsYou, normalize, matchParts and railMeta", () => {
  test("a task needs the person when a session waits or a review does", () => {
    assert.deepEqual([needsYou(task(), QUIET), needsYou(task({ pendingReviews: 1 }), QUIET), needsYou(task(), { live: 1, attention: true })], [false, true, true]);
  });

  test("normalize folds case and accents; matchParts splits around the first match", () => {
    assert.equal(normalize("Ação É"), "acao e");
    assert.deepEqual(matchParts("Café design view", "DESIGN"), { before: "Café ", match: "design", after: " view" });
    assert.deepEqual(matchParts("Café design", "cafe"), { before: "", match: "Café", after: " design" });
    assert.deepEqual(matchParts("Write docs", ""), { before: "Write docs", match: "", after: "" });
    assert.deepEqual(matchParts("Write docs", "zzz"), { before: "Write docs", match: "", after: "" });
  });

  test("a row's meta is the first that applies: reviews, waiting, N live, then the merge word on Review and Done", () => {
    const merged = { state: "merged", merged: 1, total: 1 };
    const meta = (over: Partial<Task>, activity = QUIET): string => {
      const m = railMeta(task(over), activity);
      return m.kind === "merge" ? `merge ${m.merge?.state} ${m.merge?.rail}` : m.kind;
    };
    assert.equal(meta({ status: "done", pendingReviews: 1, mergeState: merged }, { live: 2, attention: true }), "reviews");
    assert.equal(meta({ status: "done", mergeState: merged }, { live: 2, attention: true }), "waiting");
    assert.equal(meta({ status: "done", mergeState: merged }, { live: 2, attention: false }), "live");
    assert.equal(meta({ status: "done", mergeState: merged }), "merge merged merged");
    assert.equal(meta({ status: "review", mergeState: { state: "partial", merged: 1, total: 2 } }), "merge partial 1/2 merged");
    assert.equal(meta({ status: "review", mergeState: { state: "not_merged", merged: 0, total: 1 } }), "merge not_merged not merged");
    assert.equal(meta({ status: "done", mergeState: { state: "none", merged: 0, total: 0 } }), "", "none shows nothing");
    assert.equal(meta({ status: "done", mergeState: null }), "", "null shows nothing");
    assert.equal(meta({ status: "in_progress", mergeState: merged }), "", "only Review and Done carry it");
  });
});
