// The contract every TopBarSource adapter keeps (Contract: Harness server ↔
// Web UI — the top bar island, §2). Run against the HTTP and in-memory adapters.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { TopBarSource } from "../application/ports";
import { TopBarSourceError } from "../application/ports";
import { parseTopBar } from "../domain/top-bar";
import { linksWith, topBarJson, type TopBarJson } from "./fixtures";

export interface TopBarSourceHarness {
  source: TopBarSource;
  /** The server answers `model` for its path. */
  serve(model: TopBarJson): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
}

export function topBarSourceContract(name: string, make: () => TopBarSourceHarness): void {
  describe(`TopBarSource contract: ${name}`, () => {
    test("answers the model for the path", async () => {
      const h = make();
      const model = topBarJson({ path: "/projects/p1/tasks/t1", current_project_id: "p1", links: linksWith({ board: { current: true }, agent_isolation: { attention: true } }) });
      h.serve(model);
      assert.deepEqual(await h.source.load("/projects/p1/tasks/t1"), parseTopBar(model));
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

    test("a malformed path is INVALID_INPUT", async () => {
      await assert.rejects(make().source.load("not-a-path"), (err: unknown) => err instanceof TopBarSourceError && err.code === "INVALID_INPUT");
    });

    test("a network failure is NETWORK_ERROR", async () => {
      const h = make();
      h.breakNetwork();
      await assert.rejects(h.source.load("/"), (err: unknown) => err instanceof TopBarSourceError && err.code === "NETWORK_ERROR");
    });

    test("an aborted call rejects with AbortError", async () => {
      const controller = new AbortController();
      controller.abort();
      await assert.rejects(make().source.load("/", controller.signal), (err: unknown) => err instanceof Error && err.name === "AbortError");
    });
  });
}
