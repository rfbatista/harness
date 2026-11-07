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

const { serviceDTO, runDTO, listDTO } = await import("../testing/fakeGateway.js");

test("the list is read from list_services with the project and mapped", async () => {
  const calls = [];
  const api = { get: async (path, query) => (calls.push([path, query]), listDTO([serviceDTO({ status: "exited", run: runDTO({ exit_code: 2 }) })], { seq: 7, sandbox: "unavailable", runtime: { name: "podman", available: false } })) };
  const list = await servicesGateway(api).listServices("p1");
  assert.deepEqual(calls, [["/list_services", { project_id: "p1" }]]);
  assert.equal(list.seq, 7);
  assert.equal(list.sandbox, "unavailable");
  assert.deepEqual(list.runtime, { name: "podman", available: false });
  assert.equal(list.services[0].run.exit_code, 2);
});

test("a service with an unknown status is a bad response", async () => {
  const api = { get: async () => listDTO([serviceDTO({ status: "paused" })]) };
  const err = await servicesGateway(api).listServices("p1").catch((e) => e);
  assert.equal(err.code, "BAD_RESPONSE");
});

test("the feed is followed at /services/events for the project, and its events are mapped", () => {
  const seen = [];
  let handlers = null;
  const feed = { follow: (path, h) => (seen.push(path), (handlers = h), () => seen.push("stopped")) };
  const events = [];
  const stop = servicesGateway({}, feed).follow("p 1", (e) => events.push(e));
  assert.deepEqual(seen, ["/services/events?project_id=p%201"]);
  handlers.onMessage({ seq: 3, kind: "service_changed", service: serviceDTO(), running_count: 2 });
  handlers.onMessage({ seq: 4, kind: "service_deleted", service_id: "s1", running_count: 1 });
  handlers.onMessage({ nonsense: true });
  assert.deepEqual(events.map((e) => [e.seq, e.kind, e.runningCount, e.service?.name ?? e.serviceId]), [[3, "service_changed", 2, "api"], [4, "service_deleted", 1, "s1"]]);
  stop();
  assert.equal(seen.at(-1), "stopped");
});

test("the page's seed carries its list or none", () => {
  const gw = servicesGateway({}, {});
  assert.equal(gw.decodeSeed({ project_id: "p1", list: null }).list, null);
  assert.equal(gw.decodeSeed({ project_id: "p1", list: listDTO([serviceDTO()]) }).list.services.length, 1);
  assert.equal(gw.decodeSeed({ project_id: "p1" }).projectId, "p1");
});
