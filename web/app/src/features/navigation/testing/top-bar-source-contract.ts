// The contract every TopBarSource adapter keeps (Contract: Harness server ↔
// Web UI — the top bar island, §2). Run against the HTTP and in-memory adapters.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { TopBarSource } from "../application/ports";
import { TopBarSourceError } from "../application/ports";
import { linksWith, topBarFromJson, topBarJson, type TopBarJson } from "./fixtures";

export interface TopBarSourceHarness {
  source: TopBarSource;
  /** The server answers `model` for its path. */
  serve(model: TopBarJson): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
  /** The server answers `raw` (not a valid model) for every path. Adapters that read no wire have none. */
  serveMalformed?(raw: unknown): void;
}

export function topBarSourceContract(name: string, make: () => TopBarSourceHarness): void {
  describe(`TopBarSource contract: ${name}`, () => {
    test("answers the model for the path", async () => {
      const h = make();
      const model = topBarJson({ path: "/projects/p1/tasks/t1", current_project_id: "p1", links: linksWith({ board: { current: true }, agent_isolation: { attention: true } }) });
      h.serve(model);
      assert.deepEqual(await h.source.load("/projects/p1/tasks/t1"), topBarFromJson(model));
    });

    test("a path that matches no page is not an error: no current project, no current link", async () => {
      const h = make();
      const topBar = await h.source.load("/nowhere");
      assert.equal(topBar.currentProjectId, "");
      assert.equal(topBar.links.some((link) => link.current), false);
    });

    test("ignores fields and links it does not know", async () => {
      const h = make();
      h.serve(topBarJson({ path: "/x", future: 1, links: [...linksWith({}), { key: "from_the_future", href: "/f", current: false, attention: false }] }));
      assert.deepEqual((await h.source.load("/x")).links.map((link) => link.key), ["board", "projects", "agents", "credentials", "token_savers", "models", "agent_isolation", "server"]);
    });

    test("a path with a query-looking tail is carried whole, not split", async () => {
      const h = make();
      const path = "/projects/p1?tab=a&b=c d#x";
      h.serve(topBarJson({ path, current_project_id: "p1" }));
      assert.equal((await h.source.load(path)).path, path);
    });

    test("a model with no current project and no notifications part still loads", async () => {
      const h = make();
      h.serve(topBarJson({ path: "/agents", current_project_id: "", notifications: null }));
      const topBar = await h.source.load("/agents");
      assert.deepEqual([topBar.currentProjectId, topBar.path], ["", "/agents"]);
    });

    test("a malformed path is INVALID_INPUT", async () => {
      await assert.rejects(make().source.load("not-a-path"), (err: unknown) => err instanceof TopBarSourceError && err.code === "INVALID_INPUT");
    });

    test("a network failure is NETWORK_ERROR", async () => {
      const h = make();
      h.breakNetwork();
      await assert.rejects(h.source.load("/"), (err: unknown) => err instanceof TopBarSourceError && err.code === "NETWORK_ERROR");
    });

    const MALFORMED: [string, unknown][] = [
      ["a missing field", { ...topBarJson(), path: undefined }],
      ["a wrong type", topBarJson({ projects: "none" as never })],
      ["a wrong type in a link", topBarJson({ links: [{ key: "board", href: 3, current: false, attention: false }] as never })],
      ["not an object", ["a", "b"]],
    ];
    for (const [what, raw] of MALFORMED) {
      test(`${what} in the answer is BAD_RESPONSE`, async (t) => {
        const h = make();
        if (!h.serveMalformed) return t.skip("the adapter reads no wire");
        h.serveMalformed(raw);
        await assert.rejects(h.source.load("/"), (err: unknown) => err instanceof TopBarSourceError && err.code === "BAD_RESPONSE");
      });
    }

    test("an aborted call rejects with AbortError", async () => {
      const controller = new AbortController();
      controller.abort();
      await assert.rejects(make().source.load("/", controller.signal), (err: unknown) => err instanceof Error && err.name === "AbortError");
    });
  });
}
