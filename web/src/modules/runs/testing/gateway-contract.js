// The RunGateway contract (../domain/ports.js), run by every implementation.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";

/**
 * @param {string} name
 * @param {(world: object) => { gateway: import("../domain/ports.js").RunGateway, exit: (id: string, code: number) => void }} makeSubject
 */
export function runGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const world = () => ({
    sessions: ["s1"],
    commands: { r1: [{ name: "web", command: "npm run dev" }, { name: "server", command: "make air" }] },
    repositoryOf: { s1: "r1" },
  });

  contract("lists the repository's saved commands by name", async () => {
    const { gateway } = makeSubject(world());
    assert.deepEqual((await gateway.listCommands("r1")).map((c) => c.name), ["server", "web"]);
    assert.deepEqual(await gateway.listCommands("r9"), []);
  });

  contract("saves, replaces and deletes a command", async () => {
    const { gateway } = makeSubject(world());
    const saved = await gateway.saveCommand("r1", " test ", " go test ./... ");
    assert.deepEqual([saved.name, saved.command], ["test", "go test ./..."]);
    await gateway.saveCommand("r1", "test", "make test");
    assert.deepEqual((await gateway.listCommands("r1")).map((c) => `${c.name}=${c.command}`), ["server=make air", "test=make test", "web=npm run dev"]);
    await gateway.deleteCommand("r1", "web");
    assert.deepEqual((await gateway.listCommands("r1")).map((c) => c.name), ["server", "test"]);
    await assert.rejects(gateway.saveCommand("r1", "a/b", "x"), Codes.INVALID_NAME);
    await assert.rejects(gateway.deleteCommand("r1", "web"), Codes.RUN_COMMAND_NOT_FOUND);
  });

  contract("starts a saved command; starting it again returns the running run", async () => {
    const { gateway } = makeSubject(world());
    const run = await gateway.start("s1", { name: "server" });
    assert.deepEqual([run.name, run.command, run.status], ["server", "make air", "running"]);
    assert.ok(run.startedAt instanceof Date);
    assert.equal((await gateway.start("s1", { name: "server" })).id, run.id);
  });

  contract("starts a command line, lists runs newest first, stops one", async () => {
    const subject = makeSubject(world());
    const first = await subject.gateway.start("s1", { name: "server" });
    const second = await subject.gateway.start("s1", { command: "go test ./..." });
    assert.equal(second.name, "go test ./...");
    assert.deepEqual((await subject.gateway.listRuns("s1")).map((r) => r.id), [second.id, first.id]);
    assert.equal((await subject.gateway.stop(first.id)).status, "stopped");
    subject.exit(second.id, 1);
    const [latest] = await subject.gateway.listRuns("s1");
    assert.deepEqual([latest.status, latest.exitCode], ["exited", 1]);
  });

  contract("refuses an unknown saved command, nothing to run, an unknown run", async () => {
    const { gateway } = makeSubject(world());
    await assert.rejects(gateway.start("s1", { name: "nope" }), Codes.RUN_COMMAND_NOT_FOUND);
    await assert.rejects(gateway.start("s1", { command: "  " }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.stop("run-nope"), Codes.RUN_NOT_FOUND);
  });
}
