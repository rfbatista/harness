import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { servicesRuntime } from "./servicesRuntime.js";

file("services/presentation/components/servicesRuntime");

const fake = (state = { container_runtime: "docker", available: true }) => {
  const saved = [];
  return {
    saved,
    gateway: {
      getSettings: async () => state,
      setSettings: async (runtime) => (saved.push(runtime), { container_runtime: runtime, available: runtime === "docker" }),
    },
  };
};
const selectEl = (value) => ({ target: { value } });

test("it reads the runtime and whether it is found", async () => {
  const { gateway } = fake();
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  assert.equal(instance.runtime, "docker");
  assert.equal(instance.found, true);
  assert.equal(instance.status, "docker was found on this machine.");
});

test("a runtime that is not found says so and points to the other choice", async () => {
  const { gateway } = fake({ container_runtime: "podman", available: false });
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  assert.equal(instance.found, false);
  assert.equal(instance.status, "podman was not found on this machine. Image services cannot start until it is installed, or you choose docker.");
});

test("choosing a runtime saves it and shows whether the new one is found", async () => {
  const { gateway, saved } = fake();
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  await instance.choose(selectEl("podman"));
  assert.deepEqual(saved, ["podman"]);
  assert.equal(instance.runtime, "podman");
  assert.equal(instance.found, false);
  assert.equal(instance.announcement, "Container runtime saved: podman.");
  assert.equal(instance.saving, false);
});

test("choosing the runtime already held saves nothing", async () => {
  const { gateway, saved } = fake();
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  await instance.choose(selectEl("docker"));
  assert.deepEqual(saved, []);
});

test("a refused choice shows the server's message and code, and keeps the old runtime", async () => {
  const gateway = {
    getSettings: async () => ({ container_runtime: "docker", available: true }),
    setSettings: async () => { throw new StructuredError("INVALID_INPUT", "container_runtime must be docker or podman"); },
  };
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  await instance.choose(selectEl("lxc"));
  assert.equal(instance.runtime, "docker");
  assert.equal(instance.error.code, "INVALID_INPUT");
  instance.dismissError();
  assert.equal(instance.error, null);
});

test("a read that fails shows the error and leaves the select usable", async () => {
  const gateway = { getSettings: async () => { throw new StructuredError("NETWORK", "The harness server is not reachable."); }, setSettings: async () => ({}) };
  const { instance } = mount(servicesRuntime({ gateway }));
  await instance.init();
  assert.equal(instance.error.code, "NETWORK");
  assert.equal(instance.loading, false);
});
