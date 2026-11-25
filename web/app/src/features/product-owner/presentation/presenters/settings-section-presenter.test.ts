import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { OwnerSwitch } from "../../application/owner-switch";
import { ProductOwnerError } from "../../application/ports";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { NOTE, SettingsSectionPresenter } from "./settings-section-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const setup = (enabled: boolean) => {
  const server = memoryProductOwner();
  server.seedProject("p1", enabled);
  const owner = new OwnerSwitch({ gateway: server, events: server });
  const section = new SettingsSectionPresenter(owner, "p1");
  owner.attach("p1");
  return { server, section, settle: settled };
};

describe("the Settings section", () => {
  test("says the state in words, and the switch cannot be pressed until it is read", async () => {
    const { section, settle } = setup(true);
    assert.deepEqual([section.viewModel.state, section.viewModel.disabled], ["loading", true]);
    await settle();
    assert.deepEqual({ ...section.viewModel }, { state: "on", label: "Product owner is on", checked: true, disabled: false, busy: false, error: "", pageHref: "/projects/p1/product-owner", note: NOTE });
  });

  test("turns off and back on", async () => {
    const { section, settle } = setup(true);
    await settle();
    await section.toggle();
    assert.deepEqual([section.viewModel.label, section.viewModel.checked], ["Product owner is off", false]);
    await section.toggle();
    assert.equal(section.viewModel.label, "Product owner is on");
  });

  test("a refusal keeps the old state and shows the error", async () => {
    const { section, server, settle } = setup(false);
    await settle();
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    await section.toggle();
    assert.deepEqual([section.viewModel.state, section.viewModel.error], ["off", "offline"]);
  });

  test("follows the settings event: a change from another tab shows at once", async () => {
    const { section, server, settle } = setup(true);
    await settle();
    server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false, digestTime: null, timeZone: "UTC", nextDigestAt: null } });
    assert.equal(section.viewModel.state, "off");
  });
});
