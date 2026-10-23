import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { claudeGateway } from "./claude-gateway.js";

file("claude/infrastructure/claude-gateway");

const state = {
  path: "/data/claude/current",
  current: "2.1.52",
  installed: [{ version: "2.1.52", installedAt: "2026-10-08T10:00:00Z", active: true }],
  installing: { version: "2.1.60", done: 48, total: 190 },
  failed: null,
  overridden: false,
  bin: "/data/claude/current",
  keepingSessions: 0,
};

function gatewayOver(answer) {
  const { fetch, calls } = fakeFetch(() => jsonResponse(200, answer));
  return { gateway: claudeGateway(apiClient({ base: "/api", fetch })), calls };
}

test("get reads the versions, the install under way and the override", async () => {
  const { gateway, calls } = gatewayOver(state);
  assert.deepEqual(await gateway.get(), state);
  assert.equal(calls[0].url, "/api/get_managed_claude");
});

test("install and use post their version and answer the state", async () => {
  const { gateway, calls } = gatewayOver(state);
  await gateway.install("latest");
  await gateway.use("2.1.52");
  assert.deepEqual(
    calls.map((c) => [c.url, JSON.parse(c.init.body)]),
    [
      ["/api/install_managed_claude", { version: "latest" }],
      ["/api/switch_managed_claude", { version: "2.1.52" }],
    ],
  );
});

test("cancel and remove post to their endpoints and read the sessions a switch leaves behind", async () => {
  const { gateway, calls } = gatewayOver({ ...state, keepingSessions: 3 });
  await gateway.cancel();
  const after = await gateway.remove("2.1.47");
  assert.equal(after.keepingSessions, 3);
  assert.deepEqual(
    calls.map((c) => [c.url, JSON.parse(c.init.body)]),
    [
      ["/api/cancel_managed_claude_install", {}],
      ["/api/remove_managed_claude_version", { version: "2.1.47" }],
    ],
  );
});

test("releases name latest and stable", async () => {
  const { gateway } = gatewayOver({ latest: "2.1.60", stable: "2.1.52" });
  assert.deepEqual(await gateway.releases(), { latest: "2.1.60", stable: "2.1.52" });
});

test("an answer that is not the state is refused", async () => {
  const { gateway } = gatewayOver({ nope: true });
  await assert.rejects(gateway.get(), "BAD_RESPONSE");
});
