// What every product owner adapter keeps of the digest time and the digests (Contract: Harness
// server ↔ Web UI — product owner, Product owner settings and Digests).

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProductOwnerError } from "../application/ports";
import { DIGEST_PAGE_SIZE } from "../domain/digest";
import type { ProductOwnerEvent } from "../domain/thread";
import { digest, digestItem, digestRun, TIME_ZONE } from "./digest-fixtures";
import type { ProductOwnerHarness } from "./product-owner-contract";

const rejectsWith = (code: string) => (err: unknown) => err instanceof ProductOwnerError && err.code === code;

export function digestContract(name: string, make: () => ProductOwnerHarness): void {
  describe(`Digest contract: ${name}`, () => {
    const setup = () => {
      const h = make();
      h.server.seedProject("p1", true);
      h.server.seedProject("p2", true);
      return h;
    };

    test("reads the digest time, none at first, with the server's time zone", async () => {
      const h = setup();
      const settings = await h.gateway.settings("p1");
      assert.equal(settings.digestTime, null);
      assert.equal(settings.timeZone, TIME_ZONE);
    });

    test("sets the digest time and clears it with an empty time, leaving the rest as it was", async () => {
      const h = setup();
      const set = await h.gateway.updateSettings("p1", { digestTime: "08:30" });
      assert.equal(set.digestTime, "08:30");
      assert.equal(set.enabled, true);
      assert.equal((await h.gateway.settings("p1")).digestTime, "08:30");
      const cleared = await h.gateway.updateSettings("p1", { digestTime: "" });
      assert.equal(cleared.digestTime, null);
      assert.equal((await h.gateway.settings("p1")).digestTime, null);
      assert.equal((await h.gateway.settings("p2")).digestTime, null);
    });

    test("keeps the digest time when the switch changes, and the switch when the time changes", async () => {
      const h = setup();
      await h.gateway.updateSettings("p1", { digestTime: "09:00" });
      const off = await h.gateway.setEnabled("p1", false);
      assert.deepEqual([off.enabled, off.digestTime], [false, "09:00"]);
      const later = await h.gateway.updateSettings("p1", { digestTime: "10:15" });
      assert.deepEqual([later.enabled, later.digestTime], [false, "10:15"]);
    });

    test("refuses a malformed digest time and an unknown project", async () => {
      const h = setup();
      for (const bad of ["8:30", "25:00", "08:61", "noon", "08:30:00"]) await assert.rejects(h.gateway.updateSettings("p1", { digestTime: bad }), rejectsWith("INVALID_INPUT"));
      assert.equal((await h.gateway.settings("p1")).digestTime, null);
      await assert.rejects(h.gateway.updateSettings("nope", { digestTime: "08:30" }), rejectsWith("PROJECT_NOT_FOUND"));
    });

    test("reads the digests newest first, with each item's kind, link and from and to", async () => {
      const h = setup();
      const moved = digest("2026-11-22", { items: [digestItem(), digestItem({ kind: "waiting_on_person", title: "Plan", url: "/tasks/t2", from: null, to: null }), digestItem({ kind: "session_failed", title: "Run", url: "/sessions/s1", from: null, to: null })] });
      h.server.seedDigest("p1", digest("2026-11-20"));
      h.server.seedDigest("p1", moved);
      h.server.seedDigest("p1", digest("2026-11-23"));
      h.server.seedDigest("p2", digest("2026-11-21"));
      const page = await h.gateway.digests("p1");
      assert.deepEqual(page.digests.map((d) => d.date), ["2026-11-23", "2026-11-22", "2026-11-20"]);
      assert.equal(page.hasEarlier, false);
      assert.deepEqual(page.digests[1], moved);
    });

    test("reads an empty page for a project with no digest", async () => {
      const h = setup();
      assert.deepEqual(await h.gateway.digests("p1"), { digests: [], hasEarlier: false });
    });

    test("pages 30 digests at a time with `before`, without a gap or a repeat", async () => {
      const h = setup();
      for (const d of digestRun("2026-11-23", 45)) h.server.seedDigest("p1", d);
      const first = await h.gateway.digests("p1");
      assert.equal(first.digests.length, DIGEST_PAGE_SIZE);
      assert.equal(first.digests[0]!.date, "2026-11-23");
      assert.equal(first.hasEarlier, true);
      const second = await h.gateway.digests("p1", first.digests.at(-1)!.date);
      assert.equal(second.digests.length, 15);
      assert.equal(second.hasEarlier, false);
      const dates = [...first.digests, ...second.digests].map((d) => d.date);
      assert.equal(new Set(dates).size, 45);
      assert.deepEqual(dates, [...dates].sort().reverse());
    });

    test("tells its project's followers a digest was recorded, and not another project's", () => {
      const h = setup();
      const seen: ProductOwnerEvent[] = [];
      const stop = h.events.follow("p1", (e) => seen.push(e), () => {});
      h.server.recordDigest("p1", digest("2026-11-23"));
      h.server.recordDigest("p2", digest("2026-11-23"));
      stop();
      assert.deepEqual(seen, [{ type: "digest", digestId: "d-2026-11-23", date: "2026-11-23" }]);
    });

    test("fails as the server does for an unknown project", async () => {
      const h = setup();
      await assert.rejects(h.gateway.digests("nope"), rejectsWith("PROJECT_NOT_FOUND"));
    });
  });
}
