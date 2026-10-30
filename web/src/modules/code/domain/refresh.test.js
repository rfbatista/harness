import { assert, file, test } from "../../../shared/testing/test.js";
import { carryWithheld, clockTime, listLimit, noticeOf, reloadFoundNothing, staleness, updatedWord, withPage } from "./refresh.js";

file("code/domain/refresh");

const f = (path, status = "modified", added = 1, deleted = 0, extra = {}) => ({ path, status, added, deleted, oldPath: "", binary: false, ...extra });

test("a file whose stat changed is updated; one that did not is left alone", () => {
  const shown = [f("a.go"), f("b.go", "modified", 2, 1), f("c.go")];
  const listed = [f("a.go"), f("b.go", "modified", 5, 1), f("c.go")];
  assert.deepEqual(staleness(shown, listed), { updated: ["b.go"], removed: [], count: 1 });
});

test("a file the list has and the screen has not is updated, and one the list lost is removed", () => {
  const shown = [f("a.go"), f("b.go")];
  const listed = [f("a.go"), f("new.go", "untracked")];
  assert.deepEqual(staleness(shown, listed), { updated: ["new.go"], removed: ["b.go"], count: 2 });
});

test("a status change counts, a staged flag does not move a hunk", () => {
  const shown = [f("a.go", "untracked", -1, 0)];
  assert.equal(staleness(shown, [f("a.go", "added", 4, 0)]).count, 1, "git add of a new file is a new kind of change");
  assert.equal(staleness([f("a.go")], [f("a.go", "modified", 1, 0, { staged: true })]).count, 0);
});

test("an untracked file's line counts are not in the listing, so they are never the difference", () => {
  assert.equal(staleness([f("n.txt", "untracked", 12, 0)], [f("n.txt", "untracked", -1, 0)]).count, 0);
});

test("a path past what the other side read is not compared", () => {
  const shown = [f("a.go"), f("b.go")];
  // the list read further than the loaded pages: its later files are not news
  assert.equal(staleness(shown, [f("a.go"), f("b.go"), f("z.go")], { shownNext: true }).count, 0);
  // the list stopped short of the screen's last file with more pages after it: not removed either
  assert.equal(staleness([f("a.go"), f("m.go")], [f("a.go")], { listedNext: true }).count, 0);
  // but a new file inside the loaded range is news even when more pages follow
  assert.deepEqual(staleness(shown, [f("a.go"), f("aa.go"), f("b.go")], { shownNext: true }).updated, ["aa.go"]);
});

test("a list read asks for as many files as are loaded, in the server's pages", () => {
  assert.deepEqual([0, 20, 50, 120, 500].map(listLimit), [50, 50, 50, 120, 200]);
});

test("a page that comes in replaces its paths in the list and brings the newest totals and cursor", () => {
  const listing = { files: [f("a.go"), f("b.go")], next: "b.go", fileCount: 3, added: 3, deleted: 0, counts: { modified: 3 }, sections: null };
  const page = { files: [f("b.go", "modified", 9, 0), f("c.go")], next: null, fileCount: 4, added: 12, deleted: 0, counts: { modified: 4 }, sections: null };
  const next = withPage(listing, page);
  assert.deepEqual(next.files.map((x) => [x.path, x.added]), [["a.go", 1], ["b.go", 9], ["c.go", 1]]);
  assert.deepEqual([next.next, next.fileCount, next.added], [null, 4, 12]);
});

test("the notice says how many files changed since loaded, and Up to date only after a Reload that found nothing", () => {
  assert.deepEqual(noticeOf({ count: 3, upToDate: false }), { state: "", text: "3 files changed since loaded", reload: true });
  assert.equal(noticeOf({ count: 1, upToDate: true }).text, "1 file changed since loaded");
  assert.deepEqual(noticeOf({ count: 0, upToDate: true }), { state: "current", text: "Up to date", reload: false });
  assert.equal(noticeOf({ count: 0, upToDate: false }), null);
  assert.equal(reloadFoundNothing({ staleBefore: 0, changed: [] }), true);
  assert.equal(reloadFoundNothing({ staleBefore: 2, changed: [] }), false);
  assert.equal(reloadFoundNothing({ staleBefore: 0, changed: ["a.go"] }), false, "a same-size edit is something");
});

test("updated N s ago, then minutes; clock time is two digits", () => {
  assert.deepEqual([0, 4_000, 59_999, 61_000, 3_600_000].map(updatedWord), ["updated 0 s ago", "updated 4 s ago", "updated 59 s ago", "updated 1 min ago", "updated 1 h ago"]);
  assert.equal(updatedWord(-5), "updated 0 s ago");
  assert.equal(clockTime(new Date(2026, 9, 28, 9, 5)), "09:05");
});

test("a file whose hunks were too large stays not loaded in a list read that carries no hunks", () => {
  const shown = [f("big.js", "added", 9000, 0, { withheld: "too_large" }), f("a.go")];
  const listed = [f("a.go"), f("big.js", "added", 9000, 0, { withheld: null })];
  assert.deepEqual(carryWithheld(listed, shown).map((x) => x.withheld ?? null), [null, "too_large"]);
  assert.equal(carryWithheld(listed, [f("a.go")]), listed, "nothing withheld: the read is used as it is");
});
