import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryProductOwner } from "../infrastructure/memory-product-owner";
import { OwnerSwitch } from "./owner-switch";
import { ProductOwnerError } from "./ports";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const setup = async (enabled = true) => {
  const server = memoryProductOwner();
  server.seedProject("p1", enabled);
  const sw = new OwnerSwitch({ gateway: server, events: server });
  sw.attach("p1");
  await settled();
  return { server, sw };
};

describe("the product owner's switch", () => {
  test("reads whether it is on", async () => {
    assert.equal((await setup(true)).sw.state.enabled, true);
    assert.equal((await setup(false)).sw.state.enabled, false);
  });

  test("flips, and the server holds the change", async () => {
    const { sw, server } = await setup(true);
    await sw.toggle();
    assert.equal(sw.state.enabled, false);
    assert.equal((await server.settings("p1")).enabled, false);
    await sw.toggle();
    assert.equal(sw.state.enabled, true);
  });

  test("a refusal keeps the switch where it was and says why", async () => {
    const { sw, server } = await setup(true);
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    await sw.toggle();
    assert.deepEqual({ ...sw.state }, { enabled: true, busy: false, error: "offline" });
    server.failWith(null);
    await sw.toggle();
    assert.equal(sw.state.error, "");
  });

  test("follows a change made in another tab", async () => {
    const { sw, server } = await setup(true);
    server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false, digestTime: null, timeZone: "UTC", nextDigestAt: null } });
    assert.equal(sw.state.enabled, false);
  });

  test("is not flipped before it is read, and not twice at once", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const sw = new OwnerSwitch({ gateway: server, events: server });
    sw.attach("p1");
    void sw.toggle();
    assert.equal(sw.state.busy, false, "pressing before it is read does nothing");
    await settled();
    assert.equal(sw.state.enabled, true);
    const first = sw.toggle();
    await sw.toggle();
    await first;
    assert.equal(sw.state.enabled, false, "the second press, made while the first was on its way, did nothing");
  });

  test("stops following when detached", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const sw = new OwnerSwitch({ gateway: server, events: server });
    const stop = sw.attach("p1");
    assert.equal(server.followers, 1);
    stop();
    assert.equal(server.followers, 0);
  });
});
