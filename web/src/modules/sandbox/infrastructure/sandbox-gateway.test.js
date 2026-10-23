import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { sandboxGateway } from "./sandbox-gateway.js";

file("sandbox/infrastructure/sandbox-gateway");

const state = { enabled: false, driver: "seatbelt", paths: [{ path: "/opt/cache", write: true }] };

function gatewayOver(answer = state) {
  const { fetch, calls } = fakeFetch(() => jsonResponse(200, answer));
  return { gateway: sandboxGateway(apiClient({ base: "/api", fetch })), calls };
}

test("get reads the switch, the driver and the paths", async () => {
  const { gateway, calls } = gatewayOver();
  assert.deepEqual(await gateway.get(), state);
  assert.equal(calls[0].url, "/api/get_sandbox_settings");
});

test("each change posts to its endpoint and answers the new state", async () => {
  const { gateway, calls } = gatewayOver();
  await gateway.setEnabled(false);
  await gateway.addPath("/opt/cache", true);
  await gateway.setPathAccess("/opt/cache", false);
  await gateway.removePath("/opt/cache");
  assert.deepEqual(
    calls.map((c) => [c.url, JSON.parse(c.init.body)]),
    [
      ["/api/set_sandbox_enabled", { enabled: false }],
      ["/api/add_sandbox_path", { path: "/opt/cache", write: true }],
      ["/api/update_sandbox_path", { path: "/opt/cache", write: false }],
      ["/api/remove_sandbox_path", { path: "/opt/cache" }],
    ],
  );
});

test("an answer that is not the state is refused", async () => {
  const { gateway } = gatewayOver({ nope: true });
  await assert.rejects(gateway.get(), "BAD_RESPONSE");
});
