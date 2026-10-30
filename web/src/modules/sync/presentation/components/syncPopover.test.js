import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memorySync } from "../../infrastructure/memory-gateway.js";
import { makeState, T0 } from "../../testing/fixtures.js";
import { syncChip } from "./syncChip.js";
import { syncPopover } from "./syncPopover.js";
import { emptyStore, syncActions } from "./syncStore.js";

file("sync/presentation/syncPopover");

const clock = new Date(T0.getTime() + 60000);
const page = {
  projectId: "p1",
  ticketId: "t1",
  agentNames: { a1: "go-developer" },
  repositoryNames: { r1: "blog" },
  sessions: [{ id: "s1", ticketId: "t1", agentId: "a1", repositoryId: "r1", workspaceId: "ws1", branch: "agent/sync-1234" }],
};

function setup({ states = [makeState({ upstream: "ahead" })], host = page } = {}) {
  const memory = memorySync({ workspaces: states.map((state) => ({ state })), now: () => clock });
  const store = emptyStore();
  const actions = syncActions({ gateway: memory.gateway, store });
  for (const s of states) actions.keep(s);
  const mounted = mount(() => syncPopover({ store, actions, now: () => clock })(), { el: {} });
  Object.defineProperty(mounted.instance, "$data", { value: host });
  return { memory, store, ...mounted };
}

test("the button shows from the page's sessions, and the rows from the store's states", () => {
  const { instance } = setup();
  assert.deepEqual([instance.shown, instance.rows.length, instance.rows[0].agent, instance.fetches[0].label], [true, 1, "go-developer", "↻ Fetch blog"]);
  assert.equal(setup({ host: { ...page, sessions: [] } }).instance.shown, false);
});

test("opening asks the page's popup dialog to open under the button; closing it forgets a question", () => {
  const { instance, dispatched } = setup();
  assert.equal(instance.expandedAttr, "false");
  instance.openPopover();
  assert.deepEqual([instance.expandedAttr, dispatched[0].name, dispatched[0].detail.id], ["true", "dialog-open", "task-git"]);
  instance.askPush("ws1");
  instance.popoverClosed({ detail: { id: "task-reviews" } });
  assert.equal(instance.expandedAttr, "true", "another popover's close is not ours");
  instance.popoverClosed({ detail: { id: "task-git" } });
  assert.deepEqual([instance.expandedAttr, instance.confirming], ["false", ""]);
});

test("Push asks first, naming the remote branch and the commit count; confirming pushes and the row follows", async () => {
  const { instance, memory, dispatched } = setup();
  instance.askPush("ws1");
  assert.equal(instance.rows[0].question, "Push 3 commits to origin/agent/sync-1234?");
  assert.equal(instance.rows[0].confirming, true);
  await instance.confirmPush("ws1");
  await flush();
  assert.deepEqual([instance.rows[0].upstreamState, instance.rows[0].offersPush, instance.rows[0].confirming], ["in-sync", false, false]);
  assert.equal(memory.pushed.length, 1);
  assert.deepEqual(dispatched.at(-1), { name: "announce", detail: { text: "Pushed." } });
});

test("cancelling the question pushes nothing", () => {
  const { instance } = setup();
  instance.askPush("ws1");
  instance.cancelPush();
  assert.deepEqual([instance.rows[0].confirming, instance.rows[0].upstreamState], [false, "ahead"]);
});

test("Fetch is per repository: Fetching… and dimmed counts while it runs, then the new age", async () => {
  const { instance, memory } = setup({ states: [makeState({ upstream: "in_sync" })] });
  memory.remoteMoves("ws1", { ahead: 0, behind: 4 });
  const done = instance.fetch("r1");
  assert.deepEqual([instance.fetches[0].label, instance.busyAttr, instance.rows[0].upstreamLoading], ["Fetching…", "true", true]);
  await done;
  assert.deepEqual([instance.fetches[0].label, instance.busyAttr, instance.rows[0].upstreamState, instance.rows[0].upstreamBehind], ["↻ Fetch blog", null, "behind", 4]);
  assert.equal(instance.freshLine, "Fetched just now");
});

test("a session row's chip shows while a state is held and its worktree is on disk", () => {
  const store = emptyStore();
  const actions = syncActions({ gateway: memorySync({ workspaces: [] }).gateway, store });
  const chip = (id) => mount(() => syncChip({ store, actions, now: () => clock })(id), { el: {} }).instance;
  actions.keep(makeState());
  actions.keep(makeState({ workspaceId: "ws2", gone: true }));
  assert.deepEqual([chip("ws1").shown, chip("ws2").shown, chip("ws9").shown], [true, false, false]);
});
