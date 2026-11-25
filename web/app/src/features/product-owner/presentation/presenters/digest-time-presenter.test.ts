import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DigestTime } from "../../application/digest-time";
import { ProductOwnerError } from "../../application/ports";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { DigestTimePresenter, HINT } from "./digest-time-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
// 2026-11-24 07:00 in São Paulo.
const MORNING = Date.parse("2026-11-24T10:00:00Z");

const setup = async (opts: { enabled?: boolean; time?: string; now?: number } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", opts.enabled ?? true);
  if (opts.time !== undefined) await server.updateSettings("p1", { digestTime: opts.time });
  const digestTime = new DigestTime({ gateway: server, events: server });
  const block = new DigestTimePresenter(digestTime, "p1", { now: () => opts.now ?? MORNING });
  digestTime.attach("p1");
  await settled();
  return { server, block };
};

describe("the Daily digest block", () => {
  test("is loading until the settings are read", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const digestTime = new DigestTime({ gateway: server, events: server });
    const block = new DigestTimePresenter(digestTime, "p1", { now: () => MORNING });
    digestTime.attach("p1");
    assert.equal(block.viewModel.state, "loading");
    await settled();
    assert.equal(block.viewModel.state, "ready");
  });

  test("shows the time, the server's zone and the next run, today while the time is ahead", async () => {
    const { block } = await setup({ time: "09:00" });
    assert.equal(block.viewModel.value, "09:00");
    assert.equal(block.viewModel.note, "America/Sao_Paulo, the server's time zone · next: Tue 24 Nov, 09:00");
    assert.equal(block.viewModel.hint, HINT);
    assert.equal(block.viewModel.browseHref, "/projects/p1/product-owner#digests");
  });

  test("the next run is tomorrow when the time has passed in the server's zone", async () => {
    const { block } = await setup({ time: "09:00", now: Date.parse("2026-11-24T13:00:00Z") });
    assert.match(block.viewModel.note, /next: Wed 25 Nov, 09:00$/);
  });

  test("with no time set, says no digest is recorded", async () => {
    const { block } = await setup();
    assert.deepEqual([block.viewModel.value, block.viewModel.canClear], ["", false]);
    assert.equal(block.viewModel.note, "America/Sao_Paulo, the server's time zone · no digest is recorded until a time is set");
  });

  test("saves a complete time at once and moves the next run", async () => {
    const { block, server } = await setup();
    await block.change("08:30");
    assert.equal((await server.settings("p1")).digestTime, "08:30");
    assert.equal(block.viewModel.value, "08:30");
    assert.match(block.viewModel.note, /next: Tue 24 Nov, 08:30$/);
    assert.equal(block.viewModel.canClear, true);
  });

  test("waits while the time is half typed, and sends nothing", async () => {
    const { block, server } = await setup({ time: "09:00" });
    await block.change("");
    await block.change("0");
    assert.equal((await server.settings("p1")).digestTime, "09:00");
  });

  test("clears the time with Clear", async () => {
    const { block, server } = await setup({ time: "09:00" });
    await block.clear();
    assert.equal((await server.settings("p1")).digestTime, null);
    assert.deepEqual([block.viewModel.value, block.viewModel.canClear], ["", false]);
  });

  test("a refusal says why, keeps the time that holds and tells the input to go back to it", async () => {
    const { block, server } = await setup({ time: "09:00" });
    const before = block.viewModel.revision;
    server.failWith(new ProductOwnerError("INVALID_INPUT", "not a time"));
    await block.change("08:30");
    assert.deepEqual([block.viewModel.saveError, block.viewModel.value, block.viewModel.busy], ["not a time", "09:00", false]);
    assert.equal(block.viewModel.revision, before + 1);
    server.failWith(null);
    await block.change("08:30");
    assert.equal(block.viewModel.saveError, "");
  });

  test("is busy while a change is on its way, and the last time asked for meanwhile follows it", async () => {
    const { block, server } = await setup();
    const first = block.change("08:30");
    assert.equal(block.viewModel.busy, true);
    await block.change("10:00");
    await first;
    assert.equal((await server.settings("p1")).digestTime, "10:00");
    assert.equal(block.viewModel.busy, false);
  });

  test("off: dimmed, says it is paused, and can still be changed", async () => {
    const { block, server } = await setup({ enabled: false, time: "09:00" });
    assert.equal(block.viewModel.dimmed, true);
    assert.equal(block.viewModel.note, "America/Sao_Paulo, the server's time zone · paused while the product owner is off");
    await block.change("10:00");
    assert.equal((await server.settings("p1")).digestTime, "10:00");
    assert.equal(block.viewModel.dimmed, true);
  });

  test("follows the settings event: another tab's change shows at once", async () => {
    const { block, server } = await setup({ time: "09:00" });
    server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false, digestTime: "10:30", timeZone: "America/Sao_Paulo" } });
    assert.deepEqual([block.viewModel.value, block.viewModel.dimmed], ["10:30", true]);
  });

  test("a failed read says why and reads again on retry", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    const digestTime = new DigestTime({ gateway: server, events: server });
    const block = new DigestTimePresenter(digestTime, "p1", { now: () => MORNING });
    digestTime.attach("p1");
    await settled();
    assert.deepEqual([block.viewModel.state, block.viewModel.error], ["error", "offline"]);
    server.failWith(null);
    block.retry();
    await settled();
    assert.equal(block.viewModel.state, "ready");
  });

  test("reads the settings again when a digest is recorded, as no settings event says the next run moved", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    await server.updateSettings("p1", { digestTime: "09:00" });
    // The page hears no settings event: the server's settings change on their own, and only a digest event tells the page to look.
    const events = { follow: (projectId: string, onEvent: Parameters<typeof server.follow>[1], onStatus: Parameters<typeof server.follow>[2]) => server.follow(projectId, (e) => e.type !== "settings" && onEvent(e), onStatus) };
    const digestTime = new DigestTime({ gateway: server, events });
    const block = new DigestTimePresenter(digestTime, "p1", { now: () => MORNING });
    digestTime.attach("p1");
    await settled();
    await server.updateSettings("p1", { digestTime: "10:00" });
    await settled();
    assert.equal(block.viewModel.value, "09:00");
    server.recordDigest("p1", { digestId: "d", date: "2026-11-23", periodStart: new Date(), periodEnd: new Date(), summary: "", items: [] });
    await settled();
    assert.equal(block.viewModel.value, "10:00");
  });
});
