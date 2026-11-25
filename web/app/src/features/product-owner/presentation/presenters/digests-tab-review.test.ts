// Review tests for the Digests tab: gaps found in review of agent/project-product-owner-60e33acf.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProjectDigests } from "../../application/project-digests";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { digest, digestItem } from "../../testing/digest-fixtures";
import { DigestsTabPresenter } from "./digests-tab-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));

const setup = async (opts: { enabled?: boolean; digests?: ReturnType<typeof digest>[] } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", opts.enabled ?? true);
  await server.updateSettings("p1", { digestTime: "09:00" });
  for (const d of opts.digests ?? []) server.seedDigest("p1", d);
  const digests = new ProjectDigests({ gateway: server, events: server });
  const tab = new DigestsTabPresenter(digests, "p1");
  digests.attach("p1");
  await settled();
  return { server, digests, tab };
};

describe("the Digests tab (review)", () => {
  test("links an item only to a page of this web UI: an address the browser reads as another host is not a link", async () => {
    // The URL parser treats "\" as "/" and drops tabs and newlines, so each of these leaves the site.
    const offSite = ["/\\elsewhere.test/x", "/\t/elsewhere.test/x", "/\n/elsewhere.test/x"];
    const { tab } = await setup({ digests: [digest("2026-11-23", { items: offSite.map((url) => digestItem({ title: url, url })) })] });
    for (const item of tab.viewModel.selected?.sections.flatMap((s) => s.items) ?? []) {
      assert.equal(item.href, "", `${JSON.stringify(item.title)} must not be a link`);
    }
  });

  test("after the stream was down, reads the settings again: the product owner switched off meanwhile shows as off", async () => {
    const { tab, server } = await setup({ digests: [digest("2026-11-22")] });
    assert.equal(tab.viewModel.offNote, "");
    server.setStatus("paused");
    // Switched off in another tab while this one's stream was down: no settings event reaches it.
    server.seedProject("p1", false);
    server.setStatus("resynced");
    await settled();
    assert.notEqual(tab.viewModel.offNote, "");
    assert.equal(tab.viewModel.head, "Paused while the product owner is off");
  });
});
