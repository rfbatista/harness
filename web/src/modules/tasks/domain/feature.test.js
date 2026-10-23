import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { childrenOf, childrenWord, parentOf } from "./feature.js";

file("tasks/domain/feature");

const feature = makeTask({ id: "f1", title: "Exports" });
const a = makeTask({ id: "c1", title: "CSV export", parentId: "f1" });
const b = makeTask({ id: "c2", title: "PDF export", parentId: "f1" });
const orphan = makeTask({ id: "c3", title: "Lost", parentId: "gone" });
const plain = makeTask({ id: "t1" });
const tasks = [a, feature, plain, b, orphan];

test("a child's parent is found in the list; a task without one, or whose parent is not listed, has none", () => {
  assert.equal(parentOf(tasks, a), feature);
  assert.equal(parentOf(tasks, plain), null);
  assert.equal(parentOf(tasks, orphan), null);
  assert.equal(parentOf(tasks, null), null);
});

test("a feature's children keep the list's order", () => {
  assert.deepEqual(childrenOf(tasks, feature).map((t) => t.id), ["c1", "c2"]);
  assert.deepEqual(childrenOf(tasks, plain), []);
  assert.deepEqual(childrenOf(tasks, undefined), []);
});

test("children are counted in words", () => {
  assert.equal(childrenWord(0), "");
  assert.equal(childrenWord(1), "1 task");
  assert.equal(childrenWord(3), "3 tasks");
});
