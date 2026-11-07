import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { servicesGateway } from "./services-gateway.js";

file("services/infrastructure/services-gateway");

test("settings are read from get_services_settings", async () => {
  const api = { get: async (path) => (assert.equal(path, "/get_services_settings"), { container_runtime: "podman", available: false }) };
  assert.deepEqual(await servicesGateway(api).getSettings(), { container_runtime: "podman", available: false });
});

test("a runtime is saved with set_services_settings", async () => {
  const calls = [];
  const api = { post: async (path, body) => (calls.push([path, body]), { container_runtime: "docker", available: true }) };
  await servicesGateway(api).setSettings("docker");
  assert.deepEqual(calls, [["/set_services_settings", { container_runtime: "docker" }]]);
});

test("an answer without a runtime is a bad response", async () => {
  const api = { get: async () => ({}) };
  const err = await servicesGateway(api).getSettings().catch((e) => e);
  assert.ok(err instanceof StructuredError);
  assert.equal(err.code, "BAD_RESPONSE");
});
