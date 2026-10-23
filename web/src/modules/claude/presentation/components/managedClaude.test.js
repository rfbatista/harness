import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { managedClaude } from "./managedClaude.js";

file("claude/presentation/components/managedClaude");

const none = { path: "/d/current", current: "", installed: [], installing: null, failed: null, overridden: false, bin: "/d/current" };
const installed = (...versions) => ({
  ...none,
  current: versions[0],
  installed: versions.map((v, i) => ({ version: v, installedAt: "2026-10-08T10:00:00Z", active: i === 0 })),
});

let sections = 0;
function mounted(state, gateway = {}) {
  const id = `claude-seed-test-${++sections}`;
  const el = document.createElement("section");
  el.dataset.seed = id;
  el.innerHTML = `<script type="application/json" id="${id}">${JSON.stringify(state)}</script><div data-ssr>first paint</div>`;
  document.body.append(el);
  const m = mount(managedClaude({ gateway: { releases: async () => ({ latest: "2.1.60", stable: "2.1.52" }), ...gateway }, pollMs: 1 }), { el });
  m.instance.init();
  return { ...m, el };
}

test("with nothing installed it is missing, with a None chip and one action", () => {
  const { instance, el } = mounted(none);
  assert.equal(instance.isMissing, true);
  assert.equal(instance.chipText, "None");
  assert.equal(instance.chipTone, "danger");
  assert.equal(instance.releasesState, "idle");
  assert.equal(el.querySelector("[data-ssr]"), null);
});

test("an override means missing no longer blocks, and the page says which binary runs", () => {
  const { instance } = mounted({ ...none, overridden: true, bin: "/opt/dev/claude" });
  assert.equal(instance.isMissing, false);
  assert.equal(instance.overrideText, "Agents run /opt/dev/claude. The version you choose below is saved but not used.");
});

test("installed versions list with the active one marked and only the others usable", () => {
  const { instance } = mounted(installed("2.1.52", "2.1.47"));
  assert.deepEqual(instance.rows.map((r) => [r.version, r.active, r.canUse]), [["2.1.52", true, false], ["2.1.47", false, true]]);
  assert.equal(instance.chipText, "2.1.52");
});

test("the picker offers latest and stable, marking installed ones", async () => {
  const { instance } = mounted(installed("2.1.52"));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(instance.options.map((o) => [o.label, o.taken]), [["latest · 2.1.60", false], ["stable · 2.1.52 · installed", true]]);
});

test("versions that cannot be loaded say so and let the person type one", async () => {
  const { instance } = mounted(installed("2.1.52"), { releases: async () => { throw new StructuredError("UNKNOWN", "down"); } });
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(instance.releasesFailed, true);
});

test("an install is polled until it ends, showing its progress", async () => {
  const answers = [
    { ...none, installing: { version: "2.1.60", done: 48 * 1048576, total: 190 * 1048576 } },
    installed("2.1.60"),
  ];
  const gateway = { install: async () => answers[0], get: async () => answers[1] };
  const { instance } = mounted(none, gateway);
  await instance.start("latest");
  assert.equal(instance.isInstalling, true);
  assert.equal(instance.progressText, "48 MB of 190 MB");
  assert.equal(instance.cannotInstall, true);
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(instance.isInstalling, false);
  assert.equal(instance.current, "2.1.60");
  assert.equal(instance.announcement, "Installed 2.1.60.");
});

test("a failed install shows its version and reason, and Retry installs it again", async () => {
  const calls = [];
  const failed = { ...none, failed: { version: "2.1.40", message: "checksum mismatch" } };
  const { instance } = mounted(failed, { install: async (v) => (calls.push(v), failed) });
  assert.equal(instance.hasFailed, true);
  assert.equal(instance.failedTitle, "Could not install 2.1.40.");
  await instance.retry();
  assert.deepEqual(calls, ["2.1.40"]);
});

test("Use this switches and says new sessions use it", async () => {
  const calls = [];
  const { instance } = mounted(installed("2.1.47", "2.1.52"), { use: async (v) => (calls.push(v), installed("2.1.52")) });
  const row = document.createElement("li");
  row.dataset.version = "2.1.52";
  const button = document.createElement("button");
  row.append(button);
  await instance.use({ currentTarget: button });
  assert.deepEqual(calls, ["2.1.52"]);
  assert.equal(instance.current, "2.1.52");
  assert.equal(instance.switchLine, "New sessions will use 2.1.52.");
});

test("a switch names how many running sessions keep the version they started with", async () => {
  const row = document.createElement("li");
  row.dataset.version = "2.1.52";
  const button = document.createElement("button");
  row.append(button);
  for (const [n, line] of [
    [3, "New sessions will use 2.1.52. 3 running sessions keep 2.1.47."],
    [1, "New sessions will use 2.1.52. 1 running session keeps 2.1.47."],
  ]) {
    const { instance } = mounted(installed("2.1.47", "2.1.52"), { use: async () => ({ ...installed("2.1.52"), keepingSessions: n }) });
    await instance.use({ currentTarget: button });
    assert.equal(instance.switchLine, line);
  }
});

test("an installed version that is not current can be removed, the current one is not offered", async () => {
  const calls = [];
  const { instance } = mounted(installed("2.1.52", "2.1.47"), { remove: async (v) => (calls.push(v), installed("2.1.52")) });
  assert.deepEqual(instance.rows.map((r) => [r.version, r.canRemove]), [["2.1.52", false], ["2.1.47", true]]);
  const row = document.createElement("li");
  row.dataset.version = "2.1.47";
  const button = document.createElement("button");
  row.append(button);
  await instance.remove({ currentTarget: button });
  assert.deepEqual(calls, ["2.1.47"]);
  assert.deepEqual(instance.rows.map((r) => r.version), ["2.1.52"]);
  assert.equal(instance.announcement, "Removed 2.1.47.");
});

test("Cancel stops the install and leaves no install or failure on the page", async () => {
  let cancelled = false;
  const running = { ...installed("2.1.52"), installing: { version: "2.1.60", done: 10, total: 100 } };
  const { instance } = mounted(running, { cancel: async () => ((cancelled = true), installed("2.1.52")), get: async () => running });
  assert.equal(instance.isInstalling, true);
  await instance.cancel();
  assert.equal(cancelled, true);
  assert.equal(instance.isInstalling, false);
  assert.equal(instance.hasFailed, false);
  assert.equal(instance.announcement, "Install of 2.1.60 cancelled.");
});

test("the summary names the claude as the page's chips do", () => {
  assert.deepEqual(mounted(none).instance.summary, { text: "claude: not installed", tone: "danger", blocks: true });
  assert.deepEqual(mounted(installed("2.1.52")).instance.summary, { text: "claude: 2.1.52", tone: "signal", blocks: false });
  assert.deepEqual(mounted({ ...none, overridden: true, bin: "/x" }).instance.summary, { text: "claude: override", tone: "signal", blocks: false });
  assert.deepEqual(mounted({ ...none, installing: { version: "2.1.60", done: 0, total: -1 } }).instance.summary, { text: "claude: installing", tone: "", blocks: true });
});
