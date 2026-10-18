// The TokenSaverGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the server as far as
// the token saver components can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";
import { BUILT, activationsDTO, indexDTO, makeWorld, saverListDTO } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").TokenSaverGateway} gateway
 * @property {ReturnType<typeof import("../infrastructure/memory-gateway.js").memoryTokenSavers>} memory  the server's side
 */

/**
 * @param {string} name
 * @param {(world: object) => Subject} makeSubject
 */
export function tokenSaverGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);

  contract("decodes the seeds the server paints", async () => {
    const { gateway } = makeSubject(makeWorld());
    const world = makeWorld();
    assert.deepEqual(gateway.decodeActivations(activationsDTO(world.activations[0])), world.activations[0]);
    assert.deepEqual(gateway.decodeSavers(saverListDTO(world.savers)), world.savers);
    assert.deepEqual(gateway.decodeIndexes({ repository_indexes: world.indexes.map(indexDTO) }), world.indexes);
    assert.throws(() => gateway.decodeSavers({}), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeIndexes({ repository_indexes: [{ repository_id: "r1", built_at: "yesterday" }] }), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeActivations({ level: "project", owner_id: "p1", activations: [{ saver: "rtk", value: "maybe" }] }), Codes.BAD_RESPONSE);
  });

  contract("sets an activation and answers the owner's activations; refuses a bad value or owner", async () => {
    const { gateway } = makeSubject(makeWorld());
    const a = await gateway.setActivation("project", "p1", "rtk", "on");
    assert.deepEqual([a.level, a.ownerId, a.activations.map((x) => x.value)], ["project", "p1", ["on", "off", "off"]]);
    await assert.rejects(gateway.setActivation("project", "p1", "rtk", "inherit"), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setActivation("project", "gone", "rtk", "on"), Codes.PROJECT_NOT_FOUND);
  });

  contract("lists savers, installs one in the background and removes one", async () => {
    const { gateway, memory } = makeSubject(makeWorld());
    const list = await gateway.listSavers();
    assert.deepEqual(list.savers.map((s) => [s.saver, s.state, s.onSomewhere]), [["rtk", "installed", false], ["caveman", "not_installed", true], ["codegraph", "installed", false]]);
    const installing = await gateway.installSaver("caveman");
    assert.deepEqual([installing.state, installing.progress], ["installing", "Starting"]);
    await assert.rejects(gateway.removeSaver("caveman"), "TOKEN_SAVER_BUSY");
    memory.finishInstall("caveman");
    const caveman = (await gateway.listSavers()).savers.find((s) => s.saver === "caveman");
    assert.deepEqual([caveman.state, caveman.installedVersion], ["installed", "v4.0.1"]);
    const removed = await gateway.removeSaver("rtk");
    assert.deepEqual([removed.state, removed.installedVersion], ["not_installed", ""]);
    await assert.rejects(gateway.installSaver("nope"), "TOKEN_SAVER_NOT_FOUND");
  });

  contract("lists repository indexes and rebuilds one; refuses one without a checkout", async () => {
    const { gateway, memory } = makeSubject(makeWorld());
    const list = await gateway.listIndexes();
    assert.deepEqual(list.map((i) => [i.repositoryId, i.builtAt]), [["r1", BUILT], ["r2", null]]);
    const building = await gateway.rebuildIndex("r1");
    assert.equal(building.building, true);
    const later = new Date(BUILT.getTime() + 60_000);
    memory.finishBuild("r1", later);
    assert.deepEqual((await gateway.listIndexes())[0].builtAt, later);
    await assert.rejects(gateway.rebuildIndex("r2"), "INVALID_ROOT");
    await assert.rejects(gateway.rebuildIndex("r9"), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("rebuilding needs codegraph installed", async () => {
    const { gateway } = makeSubject(makeWorld());
    await gateway.removeSaver("codegraph");
    await assert.rejects(gateway.rebuildIndex("r1"), "TOKEN_SAVER_NOT_INSTALLED");
  });

  contract("sets the caveman intensity; refuses an unknown one", async () => {
    const { gateway, memory } = makeSubject(makeWorld());
    assert.equal(await gateway.setCavemanIntensity("ultra"), "ultra");
    assert.equal(memory.intensity(), "ultra");
    await assert.rejects(gateway.setCavemanIntensity(/** @type {any} */ ("loud")), Codes.INVALID_INPUT);
  });
}
