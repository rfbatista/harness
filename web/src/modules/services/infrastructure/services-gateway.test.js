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

test("start, stop and restart post the service id and map the answer", async () => {
  const calls = [];
  const api = {
    post: async (path, body) => {
      calls.push([path, body]);
      return path === "/start_service" ? { service: serviceDTO({ status: "running", run: runDTO() }), already_running: true } : { service: serviceDTO({ status: "stopped" }) };
    },
  };
  const gw = servicesGateway(api, {});
  const started = await gw.startService("s1");
  assert.equal(started.alreadyRunning, true);
  assert.equal(started.service.status, "running");
  assert.equal((await gw.stopService("s1")).status, "stopped");
  assert.equal((await gw.restartService("s1")).status, "stopped");
  assert.deepEqual(calls, [["/start_service", { service_id: "s1" }], ["/stop_service", { service_id: "s1" }], ["/restart_service", { service_id: "s1" }]]);
});

test("a refused start rejects with the server's code", async () => {
  const api = { post: async () => { throw new StructuredError("SANDBOX_UNAVAILABLE", "nothing was started"); } };
  const err = await servicesGateway(api, {}).startService("s1").catch((e) => e);
  assert.equal(err.code, "SANDBOX_UNAVAILABLE");
});

test("the log is read with its offset, or without one for the last part", async () => {
  const calls = [];
  const api = { get: async (path, q) => (calls.push([path, q]), { text: "hi\n", offset: 0, next_offset: 3, first_offset: 0, run_id: "r1" }) };
  const gw = servicesGateway(api, {});
  assert.equal((await gw.readLog("s1")).next_offset, 3);
  await gw.readLog("s1", 120);
  assert.deepEqual(calls, [["/read_service_log", { service_id: "s1", offset: undefined }], ["/read_service_log", { service_id: "s1", offset: "120" }]]);
});

test("the log feed is followed from an offset, and chunks and resets are mapped", () => {
  const seen = [];
  let handlers = null;
  const feed = { follow: (path, h) => (seen.push(path), (handlers = h), () => {}) };
  const events = [];
  servicesGateway({}, feed).followLog("s 1", 42, (e) => events.push(e));
  assert.deepEqual(seen, ["/services/log?service_id=s%201&from=42"]);
  handlers.onMessage({ kind: "chunk", run_id: "r1", offset: 42, text: "x\n" });
  handlers.onMessage({ kind: "reset", run_id: "r2" });
  handlers.onMessage({ kind: "mystery" });
  assert.deepEqual(events.map((e) => [e.kind, e.run_id, e.offset, e.text]), [["chunk", "r1", 42, "x\n"], ["reset", "r2", 0, ""]]);
});

test("create, update and delete post their payloads and map the answers", async () => {
  const calls = [];
  const api = {
    post: async (path, body) => {
      calls.push([path, body]);
      if (path === "/delete_service") return {};
      return path === "/update_service" ? { service: serviceDTO(), restart_needed: true } : { service: serviceDTO() };
    },
  };
  const gw = servicesGateway(api, {});
  const input = { name: "api", kind: "command" };
  assert.equal((await gw.createService("p1", input)).name, "api");
  const updated = await gw.updateService("s1", input);
  assert.deepEqual([updated.service.name, updated.restartNeeded], ["api", true]);
  await gw.deleteService("s1");
  assert.deepEqual(calls, [["/create_service", { project_id: "p1", service: input }], ["/update_service", { service_id: "s1", service: input }], ["/delete_service", { service_id: "s1" }]]);
});
