import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { saveController } from "./saveController.js";

file("agents/presentation/components/saveController");

/** Timers the test fires by hand. */
function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    /** Fires every pending timer and waits for what they started. */
    async fire() {
      const fns = [...pending.values()];
      pending.clear();
      for (const fn of fns) await fn();
    },
    get count() { return pending.size; },
  };
}

function setup() {
  const { gateway, server } = memoryCatalog(makeWorld());
  const timers = fakeTimers();
  let bundle = { previousName: "ux", name: "design", entries: ["lint"] };
  const states = [];
  const c = saveController({
    run: (options) => gateway.saveBundle({ previousName: bundle.previousName, bundle: { name: bundle.name, entries: bundle.entries } }, options),
    timers,
    onState: (s) => states.push(s.phase),
  });
  return { c, server, timers, states, gateway, set: (b) => { bundle = { ...bundle, ...b }; } };
}

const saveRequests = (server, dry) => server.requests.filter(([m, p, b]) => m === "POST" && p === "/save_catalog_bundle" && Boolean(b.dry_run) === dry);

test("typing schedules one debounced dry run, sent with the versions read at open", async () => {
  const { c, server, timers } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  c.edited({ dirty: true, valid: true });
  assert.equal(timers.count, 1, "the second keystroke replaced the first timer");
  assert.equal(saveRequests(server, true).length, 0);
  await timers.fire();
  const [[, , body]] = saveRequests(server, true);
  assert.deepEqual(body.base_versions, { "agents.json": "sha256:a1" });
  assert.equal(body.dry_run, true);
  assert.equal(c.state.phase, "dirty");
  assert.equal(c.state.preview.writes[0].path, "agents.json");
  assert.equal(typeof c.state.preview.writes[0].after, "string");
});

test("an invalid form is not previewed, and a clean one is not either", async () => {
  const { c, server, timers } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: false });
  c.edited({ dirty: false, valid: true });
  assert.equal(timers.count, 0);
  assert.equal(saveRequests(server, true).length, 0);
});

test("saving sends the same versions, then keeps the new ones and the change id for Undo", async () => {
  const { c, server } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  const result = await c.save();
  assert.equal(result.ok, true);
  assert.equal(saveRequests(server, false)[0][2].base_versions["agents.json"], "sha256:a1");
  assert.equal(c.state.phase, "saved");
  assert.equal(c.state.saved.summary, "Saved agents.json — bundle @design");
  assert.ok(c.state.baseVersions["agents.json"] !== "sha256:a1", "the answer's versions replace the old ones");
});

test("an outside edit makes the save a conflict; Overwrite sends it again with force", async () => {
  const { c, server } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  server.editOnDisk("agents.json");
  const refused = await c.save();
  assert.equal(refused.ok, false);
  assert.equal(c.state.phase, "conflict");
  assert.equal(c.state.conflict.changed[0].path, "agents.json");
  assert.equal(c.state.saved, null);
  const forced = await c.save({ force: true });
  assert.equal(forced.ok, true);
  assert.equal(saveRequests(server, false).at(-1)[2].force, true);
  assert.equal(c.state.phase, "saved");
});

test("no preview is sent while a conflict stands", async () => {
  const { c, server, timers } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  server.editOnDisk("agents.json");
  await c.save();
  c.edited({ dirty: true, valid: true });
  assert.equal(timers.count, 0);
  assert.equal(c.state.phase, "conflict");
});

test("a refused preview shows as an error, and the next edit clears it", async () => {
  const { c, timers, set } = setup();
  set({ entries: ["@design"] });
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  await timers.fire();
  assert.equal(c.state.phase, "error");
  assert.equal(c.state.refusal.code, "BUNDLE_CYCLE");
  c.edited({ dirty: true, valid: true });
  assert.equal(c.state.phase, "dirty");
});

test("reloading starts again from the versions just read and cancels a pending preview", async () => {
  const { c, timers, server } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  c.reload({ "agents.json": "sha256:new" });
  assert.equal(timers.count, 0);
  assert.equal(c.state.phase, "clean");
  assert.deepEqual(c.state.baseVersions, { "agents.json": "sha256:new" });
  assert.equal(saveRequests(server, true).length, 0);
});

test("a preview answer that arrives after the person typed on is dropped", async () => {
  const { c, timers } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  const firing = timers.fire();
  c.edited({ dirty: true, valid: true });
  await firing;
  assert.equal(c.state.preview, null);
});

test("an open item that changed on disk is probed: the server's refusal gives the conflict its diff, even for a clean editor", async () => {
  const { c, server } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: false, valid: true });
  server.editOnDisk("agents.json");
  await c.probe({ "agents.json": "sha256:now" });
  assert.equal(c.state.phase, "conflict");
  assert.equal(c.state.conflict.changed[0].path, "agents.json");
  assert.equal(saveRequests(server, false).length, 0, "nothing was written");
});

test("a probe the server answers with another refusal still blocks Save and names the changed file", async () => {
  const { c, set } = setup();
  set({ previousName: "ghost" });
  c.open({ "agents.json": "sha256:a1" });
  await c.probe({ "agents.json": "sha256:now" });
  assert.equal(c.state.phase, "conflict");
  assert.deepEqual(c.state.conflict.changed, [{ path: "agents.json", theirs: undefined }]);
});

test("a rebase takes the new versions, drops the stale preview and asks for it again", async () => {
  const { c, timers, server } = setup();
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  await timers.fire();
  assert.ok(c.state.preview);
  c.rebase({ "agents.json": "sha256:other" });
  assert.equal(c.state.preview, null);
  assert.equal(c.state.baseVersions["agents.json"], "sha256:other");
  assert.equal(timers.count, 1);
  await timers.fire();
  assert.equal(saveRequests(server, true).at(-1)[2].base_versions["agents.json"], "sha256:other");
});

test("no preview is asked for while saving is off, and a rebase after the file is fixed asks for the one owed", async () => {
  let off = true;
  const { gateway } = memoryCatalog(makeWorld());
  const timers = fakeTimers();
  const c = saveController({ run: (o) => gateway.saveBundle({ previousName: "ux", bundle: { name: "ux", entries: ["tdd"] } }, o), timers, blocked: () => off });
  c.open({ "agents.json": "sha256:a1" });
  c.edited({ dirty: true, valid: true });
  assert.equal(timers.count, 0);
  off = false;
  c.rebase({ "agents.json": "sha256:a1" });
  assert.equal(timers.count, 1);
});
