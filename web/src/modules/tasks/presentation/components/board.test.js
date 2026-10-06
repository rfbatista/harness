import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { makeTask } from "../../testing/fixtures.js";
import { board } from "./board.js";

file("tasks/presentation/board");

function setup({ tasks = [makeTask()], seeded = true } = {}) {
  const store = { byTask: {}, tasks, seeded };
  const el = document.createElement("main");
  el.dataset.projectId = "p1";
  const ssr = document.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  const mounted = mount(board({ store }), { el });
  mounted.instance.init();
  return { ...mounted, store, el };
}

test("renders the store's tasks as columns once seeded, and drops the server's copy", () => {
  const { instance, tick, el } = setup();
  assert.equal(instance.hasTasks, true);
  assert.equal(instance.isEmpty, false);
  assert.deepEqual(instance.columns.map((c) => c.cards.map((t) => t.title)), [[], [], ["Add SSE feed"], [], []]);
  assert.equal(instance.ready, false);
  tick();
  assert.equal(instance.ready, true);
  assert.equal(el.querySelector("[data-ssr]"), null);
});

test("the empty state shows when the last task goes, and the board when the first arrives", () => {
  const { instance, store } = setup({ tasks: [] });
  assert.deepEqual([instance.hasTasks, instance.isEmpty], [false, true]);
  store.tasks = [makeTask()];
  assert.deepEqual([instance.hasTasks, instance.isEmpty], [true, false]);
});

test("without a seed it leaves the server's rendering alone", () => {
  const { instance, tick, el } = setup({ seeded: false });
  tick();
  assert.deepEqual([instance.hasTasks, instance.isEmpty, instance.ready], [false, false, false]);
  assert.ok(el.querySelector("[data-ssr]"));
});
