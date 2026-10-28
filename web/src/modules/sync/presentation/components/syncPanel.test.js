import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { toGitStateDTO } from "../../infrastructure/dto.js";
import { memorySync } from "../../infrastructure/memory-gateway.js";
import { HEAD, makeState, T0 } from "../../testing/fixtures.js";
import { syncChip } from "./syncChip.js";
import { syncFetch } from "./syncFetch.js";
import { syncPanel } from "./syncPanel.js";
import { emptyStore, syncActions } from "./syncStore.js";
import { syncWatch } from "./syncWatch.js";

file("sync/presentation/syncPanel");

/** An element whose data-seed points at a seed, without a document (these tests run in Node too). */
const seedEl = (seed) => ({ dataset: { seed: "s" }, ownerDocument: { getElementById: () => ({ textContent: JSON.stringify(seed) }) } });

const clock = new Date(T0.getTime() + 60000);

async function setup({ states = [makeState({ upstream: "ahead" })], world = {}, now = () => clock } = {}) {
  const memory = memorySync({ workspaces: states.map((state) => ({ state })), now, ...world });
  const store = emptyStore();
  const actions = syncActions({ gateway: memory.gateway, store });
  const seed = { project_id: "p1", git_states: states.map(toGitStateDTO) };
  const watch = mount(syncWatch({ gateway: memory.gateway, actions }), { el: seedEl(seed) });
  watch.instance.init();
  await flush();
  const deps = { store, actions, now };
  const chip = (id = "ws1") => mount(() => syncChip(deps)(id), { el: {} }).instance;
  const panel = (id = "ws1") => mount(() => syncPanel(deps)(id), { el: {} });
  return { memory, store, actions, watch: watch.instance, chip, panel, fetchButton: (id = "r1") => mount(() => syncFetch(deps)(id), { el: {} }).instance };
}

test("the seed fills the chip with the state's words; a workspace the page does not know has none", async () => {
  const { chip, watch } = await setup();
  const c = chip();
  assert.deepEqual([c.has, c.state, c.upstream, c.hasCounts, c.ahead, c.behind, c.word, c.hasWord], [true, "ahead", "origin", true, 3, 0, "", false]);
  assert.deepEqual([c.loadingAttr, c.oldAttr, c.title], [null, null, "origin/agent/sync-1234"]);
  assert.deepEqual([chip("ws9").has, chip("ws9").state], [false, ""]);
  watch.destroy();
});

test("a git_state on the feed replaces what the chip holds for that workspace", async () => {
  const { chip, memory, watch } = await setup();
  memory.change("ws1", { upstream: { state: "behind", ref: "origin/agent/sync-1234", remote: "origin", ahead: 0, behind: 5 } });
  await flush();
  assert.deepEqual([chip().state, chip().behind], ["behind", 5]);
  watch.destroy();
});

test("Fetch shows Fetching… on the chip and the panel, then the new counts and age", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState({ upstream: "in_sync" })], world: { delayMs: 5 } });
  memory.remoteMoves("ws1", { ahead: 0, behind: 4 });
  const p = panel().instance;
  const done = p.fetch();
  await flush();
  assert.deepEqual([p.word, p.loadingAttr, p.fetchLabel, p.busyAttr, p.fetchDisabledAttr], ["Fetching…", "", "Fetching…", "true", "true"]);
  await done;
  assert.deepEqual([p.state, p.behind, p.word, p.loadingAttr, p.fetchLabel, p.busyAttr], ["behind", 4, "", null, "↻ Fetch", null]);
  assert.equal(p.freshLine, "Remote last fetched just now");
  assert.equal(p.freshOldAttr, null);
  watch.destroy();
});

test("a second Fetch while one is in flight does nothing; the repository is the key", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState(), makeState({ workspaceId: "ws2" })], world: { delayMs: 5 } });
  const a = panel("ws1").instance;
  const b = panel("ws2").instance;
  const first = a.fetch();
  const second = b.fetch();
  await Promise.all([first, second]);
  assert.equal(memory.calls.fetch, 1);
  assert.equal(b.word, "", "the other workspace of the repository showed it too, and is done");
  watch.destroy();
});

test("a feed echo that lands before the fetch answers is held, and the end state is one consistent state", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState({ upstream: "in_sync" })], world: { echoFirst: true } });
  memory.remoteMoves("ws1", { ahead: 0, behind: 6 });
  const p = panel().instance;
  await p.fetch();
  assert.deepEqual([p.state, p.behind, p.word, p.loadingAttr, p.fetchLabel], ["behind", 6, "", null, "↻ Fetch"]);
  watch.destroy();
});

test("a feed echo that lands before the push answers is held, and the end state is the newest the server read", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState({ upstream: "ahead" })], world: { echoFirst: true } });
  const p = panel().instance;
  p.askPush();
  const done = p.confirmPush();
  await flush();
  await done;
  assert.deepEqual([p.state, p.word, p.loadingAttr, p.confirming, p.showsPush], ["in-sync", "in sync", null, false, false]);
  assert.equal(memory.pushed.length, 1);
  watch.destroy();
});

test("while a push is in flight an echo does not show; an older echo never replaces the answer", async () => {
  const { chip, panel, memory, watch, store } = await setup({ states: [makeState({ upstream: "ahead" })], world: { delayMs: 5 } });
  const p = panel().instance;
  p.askPush();
  const done = p.confirmPush();
  await flush();
  memory.change("ws1", { uncommitted: { tracked: 7, untracked: 0, staged: 0 } }); // the watcher's echo, mid-push
  assert.equal(store.byWorkspace.ws1.uncommitted.tracked, 3, "held, not shown");
  assert.equal(chip().word, "Pushing…");
  await done;
  assert.deepEqual([chip().state, store.byWorkspace.ws1.uncommitted.tracked], ["in-sync", 7], "the newest read wins, and it is after the push");
  watch.destroy();
});

test("Push is offered only when ahead or unpushed with commits; behind it asks naming the remote branch and the count", async () => {
  const { panel, watch } = await setup();
  const p = panel().instance;
  assert.deepEqual([p.showsPush, p.pushLabel, p.showsQuestion], [true, "Push 3 commits", false]);
  p.askPush();
  assert.deepEqual([p.showsPush, p.showsQuestion, p.question], [false, true, "Push 3 commits to origin/agent/sync-1234?"]);
  p.cancelPush();
  assert.deepEqual([p.showsPush, p.showsQuestion], [true, false]);
  for (const upstream of ["in_sync", "behind", "diverged", "gone", "unknown"]) {
    const other = await setup({ states: [makeState({ upstream })] });
    const o = other.panel().instance;
    o.askPush();
    assert.deepEqual([upstream, o.showsPush, o.showsQuestion], [upstream, false, false]);
    other.watch.destroy();
  }
  const unpushed = await setup({ states: [makeState({ upstream: "none", base: { ref: "main", ahead: 2, behind: 0 } })] });
  assert.deepEqual([unpushed.panel().instance.showsPush, unpushed.panel().instance.pushLabel], [true, "Push 2 commits"]);
  watch.destroy();
  unpushed.watch.destroy();
});

test("confirming pushes at the head the person saw; the upstream is set and the strip closes", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState({ upstream: "none" })] });
  const p = panel().instance;
  p.askPush();
  await p.confirmPush();
  assert.deepEqual(memory.pushed, [{ workspaceId: "ws1", expectedHead: HEAD }]);
  assert.deepEqual([p.state, p.upstream, p.confirming, p.hasRefusal], ["in-sync", "origin", false, false]);
  watch.destroy();
});

test("a refused push shows its reason and the state the server re-read, with no force anywhere", async () => {
  const { panel, memory, watch } = await setup();
  const p = panel().instance;
  memory.changeQuietly("ws1", { upstream: { state: "diverged", ref: "origin/agent/sync-1234", remote: "origin", ahead: 3, behind: 2 } });
  p.askPush(); // the page still shows ahead: the person confirms what they saw
  await p.confirmPush();
  assert.deepEqual([p.hasRefusal, p.refusalIsStrip, p.refusalIsBanner, p.refusalCode], [true, true, false, "PUSH_REJECTED"]);
  assert.equal(p.refusalLead, "The remote refused the push: ! [rejected] (non-fast-forward). Nothing was pushed.");
  assert.deepEqual([p.state, p.showsPush, p.confirming], ["diverged", false, false], "the re-read state replaces the old one");
  assert.ok(!/force/i.test(`${p.refusalLead} ${p.refusalReason} ${p.pushLabel}`));
  p.dismissRefusal();
  assert.equal(p.hasRefusal, false);
  watch.destroy();
});

test("a moved HEAD refuses the push as PUSH_STALE and nothing is pushed", async () => {
  const { panel, memory, watch } = await setup();
  const p = panel().instance;
  memory.changeQuietly("ws1", { head: "f".repeat(40) });
  p.askPush();
  await p.confirmPush();
  assert.deepEqual([p.refusalCode, p.refusalIsStrip, memory.pushed.length, p.commit], ["PUSH_STALE", true, 0, "fffffff"]);
  watch.destroy();
});

test("a credential error shows the host and the link, and nothing else", async () => {
  const { panel, memory, watch } = await setup({ states: [makeState({ upstream: "in_sync" })] });
  memory.missCredential("ws1", { host: "gitlab.example.com", kind: "https_token" });
  const p = panel().instance;
  await p.fetch();
  assert.deepEqual([p.refusalIsBanner, p.refusalIsStrip, p.refusalLead, p.refusalReason, p.refusalCode, p.refusalHref, p.refusalLinkText], [true, false, "No credential stored for gitlab.example.com.", "", "", "/git-credentials?host=gitlab.example.com&kind=https_token", "Add one in Git credentials"]);
  assert.equal(p.busyAttr, null, "the panel is idle again");
  watch.destroy();
});

test("a failed fetch says so with git's reason and keeps the counts it had", async () => {
  const { panel, memory, watch } = await setup();
  memory.failNext(Codes.FETCH_FAILED, "Could not resolve host");
  const p = panel().instance;
  await p.fetch();
  assert.deepEqual([p.refusalLead, p.refusalReason, p.refusalCode, p.ahead], ["The fetch failed: Could not resolve host. Counts are as of the last fetch.", "", "FETCH_FAILED", 3]);
  watch.destroy();
});

test("the age line is amber past an hour and the chip's counts say so", async () => {
  const { panel, watch } = await setup({ states: [makeState({ upstream: "ahead", fetchedAt: new Date(clock.getTime() - 3 * 3600000) })] });
  const p = panel().instance;
  assert.deepEqual([p.freshLine, p.freshOldAttr, p.oldAttr], ["Remote last fetched 3 h ago", "", ""]);
  watch.destroy();
});

test("the panel opens and closes; closing drops the push question; its open state is not stored", async () => {
  const { panel, watch } = await setup();
  const p = panel().instance;
  p.toggle();
  p.askPush();
  assert.deepEqual([p.open, p.expandedAttr, p.confirming], [true, "true", true]);
  p.close();
  assert.deepEqual([p.open, p.expandedAttr, p.confirming], [false, "false", false]);
  watch.destroy();
});

test("after a reconnect every workspace is read again, catching a push during the drop", async () => {
  const { chip, memory, watch } = await setup();
  memory.changeQuietly("ws1", { upstream: { state: "in_sync", ref: "origin/agent/sync-1234", remote: "origin", ahead: 0, behind: 0 } });
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(chip().state, "in-sync");
  memory.forget("ws1");
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(chip().state, "in-sync", "a workspace that is gone keeps what it showed");
  watch.destroy();
});

test("the popover's rows read the base, the uncommitted words and Push; its foot has one Fetch per repository", async () => {
  const { chip, fetchButton, watch, memory } = await setup({ states: [makeState({ upstream: "ahead" }), makeState({ workspaceId: "ws2", repositoryId: "r2", upstream: "none", fetchedAt: null })] });
  const c = chip();
  assert.deepEqual([c.baseState, c.baseAhead, c.baseBehind, c.uncommitted, c.offersPush, c.branch], ["diverged", 3, 8, "3 changed · 2 new", true, "agent/sync-1234"]);
  const f = fetchButton("r1");
  assert.deepEqual([f.label, f.freshLine, f.freshOldAttr], ["↻ Fetch", "Fetched 13 min ago", null]);
  assert.deepEqual([fetchButton("r2").freshLine, fetchButton("r2").freshOldAttr], ["Not fetched yet", ""]);
  await f.fetch();
  assert.equal(memory.calls.fetch, 1);
  assert.equal(f.freshLine, "Fetched just now");
  watch.destroy();
});

test("a state older than the one held never replaces it", async () => {
  const { chip, actions, memory, watch } = await setup();
  memory.change("ws1", { uncommitted: { tracked: 9, untracked: 0, staged: 0 } });
  await flush();
  actions.keep(makeState({ readAt: new Date(T0.getTime() - 60000), upstream: "none" }));
  assert.deepEqual([chip().state, chip().gitState.uncommitted.tracked], ["ahead", 9]);
  watch.destroy();
});

test("a panel for a workspace the page did not seed reads its state once, and a page with no state for it shows no chip", async () => {
  const memory = memorySync({ workspaces: [{ state: makeState({ upstream: "ahead" }) }], now: () => clock });
  const store = emptyStore();
  const actions = syncActions({ gateway: memory.gateway, store });
  const deps = { store, actions, now: () => clock };
  const known = mount(() => syncPanel(deps)("ws1"), { el: {} }).instance;
  const unknown = mount(() => syncPanel(deps)("ws9"), { el: {} }).instance;
  assert.equal(known.has, false);
  known.init();
  unknown.init();
  await flush();
  assert.deepEqual([known.has, known.state, known.ahead], [true, "ahead", 3]);
  assert.equal(unknown.has, false);
  const again = mount(() => syncPanel(deps)("ws1"), { el: {} }).instance;
  again.init();
  await flush();
  assert.equal(memory.calls.get, 2, "ws1 was read once, ws9 once; a state already held is not read again");
});

test("the panel opens from its chip through the dialog, and closing it for any reason forgets the push question", async () => {
  const { panel, watch } = await setup();
  const mounted = panel();
  const p = mounted.instance;
  const anchor = { id: "chip" };
  p.openPanel({ currentTarget: anchor });
  assert.deepEqual([p.open, p.expandedAttr], [true, "true"]);
  assert.deepEqual(mounted.dispatched.at(-1), { name: "dialog-open", detail: { id: p.dialogId, anchor } });
  p.askPush();
  assert.equal(p.confirming, true);
  p.panelClosed({ detail: { id: "somebody-else" } });
  assert.equal(p.open, true, "another dialog's close is not ours");
  p.panelClosed({ detail: { id: p.dialogId } });
  assert.deepEqual([p.open, p.confirming, p.expandedAttr], [false, false, "false"]);
  watch.destroy();
});

test("the History head's phrase says how long ago the remote was fetched, or that it never was", async () => {
  const { panel, watch } = await setup({ states: [makeState({ upstream: "ahead", fetchedAt: new Date(clock.getTime() - 12 * 60000) }), makeState({ workspaceId: "ws2", upstream: "unknown", fetchedAt: null })] });
  assert.equal(panel("ws1").instance.fetchedPhrase, "fetched 12 min ago");
  assert.equal(panel("ws2").instance.fetchedPhrase, "not fetched yet");
  watch.destroy();
});
