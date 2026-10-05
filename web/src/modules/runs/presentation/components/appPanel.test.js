import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { Codes } from "../../../../shared/domain/errors.js";
import { memoryRuns } from "../../infrastructure/memory-gateway.js";
import { appPanel } from "./appPanel.js";

file("runs/presentation/appPanel");

async function setup(world = {}) {
  const memory = memoryRuns({
    sessions: ["s1"],
    repositoryOf: { s1: "r1" },
    commands: { r1: [{ name: "server", command: "make air" }, { name: "web", command: "npm run dev" }] },
    ...world,
  });
  const mounted = mount(() => appPanel({ gateway: memory.gateway })({ sessionId: "s1", repositoryId: "r1" }));
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
