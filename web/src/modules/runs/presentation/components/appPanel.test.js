import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { Codes } from "../../../../shared/domain/errors.js";
import { memoryRuns } from "../../infrastructure/memory-gateway.js";
import { appPanel } from "./appPanel.js";

file("runs/presentation/appPanel");

async function setup(world = {}, { el, prefs } = {}) {
  const memory = memoryRuns({
    sessions: ["s1"],
    repositoryOf: { s1: "r1" },
    commands: { r1: [{ name: "server", command: "make air" }, { name: "web", command: "npm run dev" }] },
    dirOf: { s1: "/w/api/.worktrees/feed" },
    ...world,
  });
  const mounted = mount(() =>
    appPanel({ gateway: memory.gateway, prefs })({ sessionId: "s1", repositoryId: "r1", repositoryName: "api", branch: "agent/feed-1a2b", worktree: "/w/api/.worktrees/feed" }),
    { el },
  );
  await mounted.instance.init();
  return { ...mounted, memory };
}

test("starts on the first saved command, with nothing run yet", async () => {
  const { instance } = await setup();
  assert.equal(instance.choice, "server");
  assert.equal(instance.chosenCommand, "make air");
  assert.ok(instance.showsEmpty);
  assert.deepEqual(instance.runIds, []);
});

test("runs the chosen saved command and puts its terminal on screen", async () => {
  const { instance } = await setup();
  await instance.start();
  assert.deepEqual(instance.runIds, ["run-1"]);
  assert.ok(instance.running);
  assert.equal(instance.runLabel, "server");
  assert.equal(instance.runStatusWord, "running");
});

test("runs a typed command; it cannot run empty", async () => {
  const { instance } = await setup({ commands: {} });
  assert.ok(instance.typing, "with no saved commands, the panel types one");
  assert.ok(instance.cannotRun);
  instance.typed = "go run ./cmd/server";
  await instance.start();
  assert.equal(instance.run.command, "go run ./cmd/server");
});

test("stop, then restart runs the same command again as a new run", async () => {
  const { instance } = await setup();
  await instance.start();
  await instance.stop();
  assert.equal(instance.runStatusWord, "stopped");
  await instance.restart();
  assert.deepEqual(instance.runIds, ["run-2"]);
  assert.ok(instance.running);
  assert.ok(instance.hasHistory);
  assert.deepEqual(instance.history.map((h) => [h.id, h.word]), [["run-2", "running"], ["run-1", "stopped"]]);
});

test("restarting a running run stops it first", async () => {
  const { instance, memory } = await setup();
  await instance.start();
  await instance.restart();
  const runs = await memory.gateway.listRuns("s1");
  assert.deepEqual(runs.map((r) => r.status), ["running", "stopped"]);
});

test("an exit is picked up on refresh, with its code", async () => {
  const { instance, memory } = await setup();
  await instance.start();
  memory.exit("run-1", 2);
  await instance.refresh();
  assert.deepEqual([instance.runStatusState, instance.runStatusWord], ["failed", "exited (code 2)"]);
});

test("saves a typed command under a name and chooses it; forgets one", async () => {
  const { instance } = await setup();
  instance.choice = "";
  instance.typed = "make test";
  instance.saveName = "test";
  await instance.save();
  assert.equal(instance.choice, "test");
  assert.deepEqual(instance.commands.map((c) => c.name), ["server", "test", "web"]);
  await instance.forget();
  assert.deepEqual(instance.commands.map((c) => c.name), ["server", "web"]);
  assert.equal(instance.choice, "server");
});

test("the command field: a saved command chooses it, typed text is the command to run", async () => {
  const { instance } = await setup();
  instance.commandPicked({ detail: { value: "make lint", created: true } });
  assert.ok(instance.typing);
  assert.equal(instance.typed, "make lint");
  assert.ok(!instance.canForget, "nothing saved to remove");
  instance.commandPicked({ detail: { value: "web", created: false } });
  assert.deepEqual([instance.choice, instance.typed, instance.chosenCommand], ["web", "", "npm run dev"]);
  instance.commandPicked({ detail: { value: "", created: false } });
  assert.ok(instance.typing, "a cleared field is a command to type");
  assert.ok(instance.cannotRun);
});

test("typing in the command field makes the text the command to run", async () => {
  const { instance } = await setup();
  const input = document.createElement("input");
  input.value = "make lint";
  instance.commandTyped({ target: input });
  assert.deepEqual([instance.choice, instance.typed, instance.cannotRun], ["", "make lint", false]);
  instance.commandTyped({ target: document.createElement("select") });
  assert.equal(instance.typed, "make lint", "the select's own input event is not typing");
});

test("after a save the field shows the saved command once its option exists", async () => {
  // As the page has it: the select's x-ref belongs to the combobox's own
  // x-data, so it is not in the panel's $refs.
  const el = document.createElement("section");
  el.innerHTML = `<div x-data="combobox"><select x-ref="command"><option value="server">server</option><option value="test">test</option></select></div>`;
  const select = el.querySelector("select");
  const { instance, tick } = await setup({}, { el });
  instance.commandPicked({ detail: { value: "make test", created: true } });
  instance.saveName = "test";
  await instance.save();
  tick();
  assert.equal(select.value, "test");
});

test("a refused name shows the error with its code", async () => {
  const { instance } = await setup();
  instance.choice = "";
  instance.typed = "make test";
  instance.saveName = "a/b";
  await instance.save();
  assert.equal(instance.error.code, Codes.INVALID_NAME);
});

test("reopening the panel shows the session's running run", async () => {
  const { memory } = await setup();
  await memory.gateway.start("s1", { name: "web" });
  const again = mount(() => appPanel({ gateway: memory.gateway })({ sessionId: "s1", repositoryId: "r1" }));
  await again.instance.init();
  assert.equal(again.instance.runLabel, "web");
});

test("says which repository's code a run uses: name, branch, worktree", async () => {
  const { instance } = await setup();
  assert.deepEqual([instance.repositoryLabel, instance.branch, instance.hasBranch], ["api", "agent/feed-1a2b", true]);
  assert.equal(instance.runDir, "/w/api/.worktrees/feed", "before a run, the session's worktree");
  await instance.start();
  assert.equal(instance.runDir, "/w/api/.worktrees/feed", "a run says where it runs");
});

test("without a known name, the repository reads as its id", () => {
  const { instance } = mount(() => appPanel({ gateway: memoryRuns().gateway })({ sessionId: "s1", repositoryId: "r1" }));
  assert.deepEqual([instance.repositoryLabel, instance.hasBranch], ["r1", false]);
});

test("tells the page which run is on screen, so the tab row shows its terminal's tools", async () => {
  const { instance, dispatched } = await setup();
  instance.reportRun();
  await instance.start();
  instance.reportRun();
  assert.deepEqual(dispatched.filter((d) => d.name === "run-shown").map((d) => d.detail), [{ id: "" }, { id: "run-1" }]);
});

// --- the run worktree picker ---

const worktrees = () => ({
  s1: [
    { key: "own", kind: "own", branch: "agent/feed-1a2b", dir: "/w/api/.worktrees/feed", sessionId: "s1", agent: "", sessionStatus: "running" },
    { key: "main", kind: "main", branch: "main", dir: "/w/api", sessionId: "", agent: "", sessionStatus: "" },
    { key: "session:s2", kind: "session", branch: "agent/other", dir: "/w/api/.worktrees/other", sessionId: "s2", agent: "go-developer", sessionStatus: "running" },
  ],
});

/** A browser's preferences, in memory. */
function memoryPrefs(initial = {}) {
  const map = new Map(Object.entries(initial));
  return { map, get: (k) => map.get(k) ?? null, set: (k, v) => (v === null ? map.delete(k) : map.set(k, v)) };
}

const pick = (instance, key) => instance.worktreePicked({ detail: { value: key } });

test("the picker offers the server's worktrees in its order and starts on the session's own", async () => {
  const { instance } = await setup({ worktrees: worktrees() });
  assert.equal(instance.worktreesState, "ready");
  assert.deepEqual(instance.worktreeOptions.map((o) => [o.key, o.label, o.description]), [
    ["own", "agent/feed-1a2b", "This session's worktree"],
    ["main", "main", "Main checkout · your own working copy"],
    ["session:s2", "agent/other", "go-developer · running"],
  ]);
  assert.deepEqual(instance.ownAndMainOptions.map((o) => o.key), ["own", "main"]);
  assert.deepEqual(instance.sessionOptions.map((o) => [o.key, o.tone]), [["session:s2", "signal"]]);
  assert.equal(instance.sessionGroupLabel, "Other sessions in api");
  assert.equal(instance.pickedKey, "own");
  assert.equal(instance.pickPath, "/w/api/.worktrees/feed");
});

test("a saved command runs in the picked worktree; the run names it and says where the code comes from", async () => {
  const { instance } = await setup({ worktrees: worktrees() });
  pick(instance, "main");
  assert.equal(instance.pickIsMain, true);
  assert.deepEqual([instance.pickBranch, instance.pickPath], ["main", "/w/api"]);
  assert.ok(instance.pickTail.includes("No env files are written there"));
  assert.equal(instance.announced, "Runs in Main checkout, branch main.");
  await instance.start();
  assert.deepEqual([instance.run.dir, instance.run.worktree.key, instance.runBadge, instance.runBadgeIsMain], ["/w/api", "main", "Main checkout · main", true]);
  assert.equal(instance.runDir, "/w/api");
});

test("a typed command line uses the same pick", async () => {
  const { instance } = await setup({ commands: {}, worktrees: worktrees() });
  pick(instance, "session:s2");
  instance.typed = "make test";
  await instance.start();
  assert.deepEqual([instance.run.dir, instance.runBadge], ["/w/api/.worktrees/other", "go-developer · agent/other"]);
  assert.ok(instance.pickTail.includes("go-developer session (running)"));
});

test("the default run says it is this session's, so nobody has to remember it", async () => {
  const { instance } = await setup({ worktrees: worktrees() });
  await instance.start();
  assert.equal(instance.runBadge, "This session · agent/feed-1a2b");
  assert.equal(instance.runBadgeIsMain, false);
});

test("a saved command already running in the picked worktree is reopened, in another it starts a second run", async () => {
  const { instance } = await setup({ worktrees: worktrees() });
  await instance.start();
  await instance.start();
  assert.equal(instance.runs.length, 1);
  assert.equal(instance.notice, "“server” is already running in this worktree. Showing that run.");
  pick(instance, "main");
  await instance.start();
  assert.equal(instance.runs.length, 2);
  assert.equal(instance.notice, "");
  assert.deepEqual(instance.history.map((h) => [h.label, h.tag]), [["server", "Main checkout"], ["server", "This session"]]);
});

test("history carries no tag while every run is in one worktree", async () => {
  const { instance } = await setup({ worktrees: worktrees() });
  await instance.start();
  instance.commandPicked({ detail: { value: "make lint", created: true } });
  await instance.start();
  assert.deepEqual(instance.history.map((h) => h.tag), ["", ""]);
});

test("the pick is remembered per session, and picking the session's own forgets it", async () => {
  const prefs = memoryPrefs();
  const first = await setup({ worktrees: worktrees() }, { prefs });
  pick(first.instance, "main");
  assert.equal(prefs.get("app-run-worktree:s1"), "main");
  const again = await setup({ worktrees: worktrees() }, { prefs });
  assert.equal(again.instance.pickedKey, "main", "restored once the list has loaded");
  assert.equal(again.instance.hasFallback, false);
  pick(again.instance, "own");
  assert.equal(prefs.get("app-run-worktree:s1"), null);
  assert.equal(prefs.map.size, 0);
});

test("a remembered worktree that is gone falls back to the session's own, once", async () => {
  const prefs = memoryPrefs({ "app-run-worktree:s1": "session:gone" });
  const { instance } = await setup({ worktrees: worktrees() }, { prefs });
  assert.equal(instance.pickedKey, "own");
  assert.equal(prefs.get("app-run-worktree:s1"), null);
  assert.equal(instance.fallback.message, "That worktree no longer exists. Running in this session's worktree.");
  instance.dismissFallback();
  assert.equal(instance.hasFallback, false);
  await instance.start();
  assert.equal(instance.run.worktree.key, "own", "Run stays enabled");
});

test("a pick that drops out of a later read falls back and names what went", async () => {
  const { instance, memory } = await setup({ worktrees: worktrees() });
  pick(instance, "session:s2");
  memory.dropWorktree("s1", "session:s2");
  await instance.reopened();
  assert.equal(instance.pickedKey, "own");
  assert.equal(instance.fallback.message, "The worktree of go-developer on agent/other no longer exists. Running in this session's worktree.");
});

test("a worktree that vanished between listing and Run is refused: nothing starts, the list is read again, no second banner", async () => {
  const { instance, memory } = await setup({ worktrees: worktrees() });
  pick(instance, "session:s2");
  memory.dropWorktree("s1", "session:s2");
  await instance.start();
  assert.equal(instance.error.code, Codes.WORKTREE_NOT_AVAILABLE);
  assert.equal(instance.runs.length, 0);
  assert.equal(instance.pickedKey, "own");
  assert.equal(instance.hasFallback, false);
  assert.deepEqual(instance.worktreeOptions.map((o) => o.key), ["own", "main"]);
  await instance.start();
  assert.equal(instance.error, null);
  assert.equal(instance.run.worktree.key, "own");
});

test("while the list loads or when it fails, Run works and sends no worktree", async () => {
  const memory = memoryRuns({ sessions: ["s1"], repositoryOf: { s1: "r1" }, commands: { r1: [{ name: "server", command: "make air" }] }, dirOf: { s1: "/w/own" } });
  const failing = { ...memory.gateway, listWorktrees: async () => { throw new Error("down"); } };
  const sent = [];
  const gateway = { ...failing, start: (id, what) => (sent.push(what.worktree), memory.gateway.start(id, what)) };
  const { instance } = mount(() => appPanel({ gateway })({ sessionId: "s1", repositoryId: "r1" }));
  assert.equal(instance.worktreesLoading, "true");
  await instance.init();
  assert.equal(instance.worktreesState, "failed");
  assert.equal(instance.worktreesError, "Could not load worktrees.");
  assert.equal(instance.worktreesLoading, null);
  await instance.start();
  assert.deepEqual(sent, [""]);
  assert.equal(instance.run.worktree.key, "own");
});

test("the picker's select follows the pick once its options exist", async () => {
  const el = document.createElement("section");
  el.innerHTML = `<select id="run-worktree"><option value="own">own</option><option value="main">main</option></select>`;
  const prefs = memoryPrefs({ "app-run-worktree:s1": "main" });
  const { instance, tick } = await setup({ worktrees: worktrees() }, { el, prefs });
  tick();
  assert.equal(el.querySelector("select").value, "main");
});

test("a list that fails to re-read leaves the pick and the list as they were", async () => {
  const memory = memoryRuns({ sessions: ["s1"], repositoryOf: { s1: "r1" }, commands: { r1: [{ name: "server", command: "make air" }] }, worktrees: worktrees() });
  let down = false;
  const gateway = { ...memory.gateway, listWorktrees: (id) => (down ? Promise.reject(new Error("down")) : memory.gateway.listWorktrees(id)) };
  const { instance } = mount(() => appPanel({ gateway })({ sessionId: "s1", repositoryId: "r1" }));
  await instance.init();
  pick(instance, "main");
  down = true;
  await instance.reopened();
  assert.equal(instance.worktreesState, "ready");
  assert.equal(instance.startKey, "main");
});

test("a remembered pick waits for the first list that arrives", async () => {
  const prefs = memoryPrefs({ "app-run-worktree:s1": "main" });
  const memory = memoryRuns({ sessions: ["s1"], repositoryOf: { s1: "r1" }, commands: {}, worktrees: worktrees() });
  let down = true;
  const gateway = { ...memory.gateway, listWorktrees: (id) => (down ? Promise.reject(new Error("down")) : memory.gateway.listWorktrees(id)) };
  const { instance } = mount(() => appPanel({ gateway, prefs })({ sessionId: "s1", repositoryId: "r1" }));
  await instance.init();
  assert.equal(instance.worktreesState, "failed");
  down = false;
  await instance.reopened();
  assert.deepEqual([instance.worktreesState, instance.pickedKey], ["ready", "main"]);
});
