import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toService } from "./dto.js";
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

const { serviceDTO, runDTO, listDTO, requestDTO } = await import("../testing/fakeGateway.js");

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

test("start posts a run worktree only when one is given", async () => {
  const calls = [];
  const api = { post: async (path, body) => (calls.push(body), { service: serviceDTO({ status: "running", run: runDTO() }) }) };
  const gw = servicesGateway(api, {});
  await gw.startService("s1");
  await gw.startService("s1", "");
  await gw.startService("s1", "session:abc");
  assert.deepEqual(calls, [{ service_id: "s1" }, { service_id: "s1" }, { service_id: "s1", worktree: "session:abc" }]);
});

test("a run carries its run worktree key and its overrides", () => {
  const withRun = { ...serviceDTO({ status: "running" }), run: runDTO({ worktree_key: "session:s2", overrides: [{ name: "LOG_LEVEL", by: "go-developer · session abcdef12" }] }) };
  const run = toService(withRun).run;
  assert.equal(run.worktree_key, "session:s2");
  assert.deepEqual(run.overrides, [{ name: "LOG_LEVEL", by: "go-developer · session abcdef12" }]);
  const plain = toService({ ...serviceDTO({ status: "running" }), run: runDTO() }).run;
  assert.equal(plain.worktree_key, "");
  assert.deepEqual(plain.overrides, []);
});

test("the run worktrees of a service are read from list_service_run_worktrees", async () => {
  const calls = [];
  const api = {
    get: async (path, q) => (calls.push([path, q]), {
      worktrees: [
        { key: "main", kind: "main", branch: "main", dir: "/r", session_id: "", agent: "", session_status: "" },
        { key: "session:s2", kind: "session", branch: "", dir: "/w", session_id: "s2", agent: "go-developer", session_status: "running" },
      ],
    }),
  };
  const list = await servicesGateway(api, {}).listRunWorktrees("s1");
  assert.deepEqual(calls, [["/list_service_run_worktrees", { service_id: "s1" }]]);
  assert.deepEqual(list.map((w) => [w.key, w.kind, w.branch, w.sessionId, w.agent, w.sessionStatus]), [["main", "main", "main", "", "", ""], ["session:s2", "session", "", "s2", "go-developer", "running"]]);
});

test("a run worktree list that is not a list is a bad response", async () => {
  const err = await servicesGateway({ get: async () => ({}) }, {}).listRunWorktrees("s1").catch((e) => e);
  assert.equal(err.code, "BAD_RESPONSE");
});

test("the variables of a run are read from get_run_variables", async () => {
  const calls = [];
  const api = { get: async (path, q) => (calls.push([path, q]), { run_id: "r1", variables: [{ name: "PORT", value: "8080", source: "service", by: "" }, { name: "LOG", value: "x", source: "override", by: "go-developer · session abcdef12" }] }) };
  const read = await servicesGateway(api, {}).getRunVariables("s1");
  assert.deepEqual(calls, [["/get_run_variables", { service_id: "s1" }]]);
  assert.equal(read.runId, "r1");
  assert.deepEqual(read.variables, [{ name: "PORT", value: "8080", source: "service", by: "" }, { name: "LOG", value: "x", source: "override", by: "go-developer · session abcdef12" }]);
});

test("run variables with an unknown source, or no list, are a bad response", async () => {
  const bad = (body) => servicesGateway({ get: async () => body }, {}).getRunVariables("s1").catch((e) => e.code);
  assert.equal(await bad({ run_id: "r1", variables: [{ name: "A", value: "1", source: "mystery" }] }), "BAD_RESPONSE");
  assert.equal(await bad({}), "BAD_RESPONSE");
  assert.deepEqual((await servicesGateway({ get: async () => ({ run_id: "", variables: [] }) }, {}).getRunVariables("s1")).variables, []);
});

test("a run on a branch carries its short commit and whether it is remote", () => {
  const run = toService({ ...serviceDTO({ status: "running" }), run: runDTO({ where: { kind: "branch", dir: "/s", branch: "feature/x", sha: "abc1234", remote: true } }) }).run;
  assert.deepEqual([run.where.kind, run.where.sha, run.where.remote], ["branch", "abc1234", true]);
  const main = toService({ ...serviceDTO({ status: "running" }), run: runDTO() }).run;
  assert.deepEqual([main.where.sha, main.where.remote], ["", false]);
});

test("the branches of a service are read from list_service_branches, the query only when typed", async () => {
  const calls = [];
  const api = { get: async (path, q) => (calls.push([path, q]), { branches: [{ name: "feature/x", remote: false, sha: "abc1234", subject: "Add", committed_at: "2026-11-07T12:00:00Z" }, { name: "dev", remote: true, sha: "def5678", subject: "", committed_at: "" }], truncated: true }) };
  const gw = servicesGateway(api, {});
  const list = await gw.listBranches("s1");
  await gw.listBranches("s1", "fea");
  assert.deepEqual(calls, [["/list_service_branches", { service_id: "s1" }], ["/list_service_branches", { service_id: "s1", query: "fea" }]]);
  assert.deepEqual(list.branches.map((b) => [b.name, b.remote, b.sha, b.subject, b.committedAt]), [["feature/x", false, "abc1234", "Add", "2026-11-07T12:00:00Z"], ["dev", true, "def5678", "", ""]]);
  assert.equal(list.truncated, true);
});

test("a branch list that is not a list, or a branch without a name, is a bad response", async () => {
  const bad = (body) => servicesGateway({ get: async () => body }, {}).listBranches("s1").catch((e) => e.code);
  assert.equal(await bad({}), "BAD_RESPONSE");
  assert.equal(await bad({ branches: [{ remote: false }] }), "BAD_RESPONSE");
  assert.deepEqual((await servicesGateway({ get: async () => ({ branches: [], truncated: false }) }, {}).listBranches("s1")).branches, []);
});

test("a service carries who created it and how many requests wait on it; without them it is the person's, with none", () => {
  const made = toService({ ...serviceDTO(), created_by: { kind: "session", session_id: "abcdef12-x", task_id: "t1", agent: "go-developer" }, pending_requests: 2 });
  assert.deepEqual([made.createdBy.kind, made.createdBy.agent, made.createdBy.sessionId, made.createdBy.taskId, made.pendingRequests], ["session", "go-developer", "abcdef12-x", "t1", 2]);
  const plain = toService(serviceDTO());
  assert.deepEqual([plain.createdBy.kind, plain.createdBy.agent, plain.pendingRequests], ["person", "", 0]);
});

test("service requests are read from service_requests, the filters only when given, and mapped", async () => {
  const calls = [];
  const api = { get: async (path, q) => (calls.push([path, q]), { requests: [requestDTO({ reason: "the port clashes", changes: [{ field: "command.line", before: "make dev", after: "npm run dev" }], env: [{ name: "PORT", value: "8080", change: "set", previous_set: true }] })] }) };
  const gw = servicesGateway(api, {});
  const list = await gw.listRequests("p1");
  await gw.listRequests("p1", { state: "pending", serviceId: "s1" });
  assert.deepEqual(calls, [["/service_requests", { project_id: "p1" }], ["/service_requests", { project_id: "p1", state: "pending", service_id: "s1" }]]);
  const r = list[0];
  assert.deepEqual([r.id, r.serviceId, r.serviceName, r.action, r.agent, r.sessionId, r.taskId, r.state, r.reason, r.summary], ["q1", "s1", "api", "update", "go-developer", "abcdef12-x", "t1", "pending", "the port clashes", "update command service api"]);
  assert.deepEqual(r.changes, [{ field: "command.line", before: "make dev", after: "npm run dev" }]);
  assert.deepEqual(r.env, [{ name: "PORT", value: "8080", change: "set", previousSet: true }]);
});

test("a request without an id or with an unknown action or state is a bad response", async () => {
  const bad = (r) => servicesGateway({ get: async () => ({ requests: [r] }) }, {}).listRequests("p1").catch((e) => e.code);
  assert.equal(await bad({ ...requestDTO(), id: "" }), "BAD_RESPONSE");
  assert.equal(await bad(requestDTO({ action: "delete" })), "BAD_RESPONSE");
  assert.equal(await bad(requestDTO({ state: "mystery" })), "BAD_RESPONSE");
  assert.equal(await servicesGateway({ get: async () => ({}) }, {}).listRequests("p1").catch((e) => e.code), "BAD_RESPONSE");
});

test("an answer posts the decision, and the note only when there is one", async () => {
  const calls = [];
  const api = { post: async (path, body) => (calls.push([path, body]), { request: requestDTO({ state: "done" }) }) };
  const gw = servicesGateway(api, {});
  const done = await gw.answerRequest("q 1", "approve");
  await gw.answerRequest("q1", "deny", "not now");
  assert.deepEqual(calls, [["/service_requests/q%201/answer", { decision: "approve" }], ["/service_requests/q1/answer", { decision: "deny", note: "not now" }]]);
  assert.equal(done.state, "done");
});

test("a service_request feed event carries the request without values and no running count", () => {
  let handlers = null;
  const feed = { follow: (path, h) => ((handlers = h), () => {}) };
  const events = [];
  servicesGateway({}, feed).follow("p1", (e) => events.push(e));
  handlers.onMessage({ seq: 5, kind: "service_request", request: requestDTO({ env: [{ name: "PORT", change: "set", previous_set: false }] }) });
  handlers.onMessage({ seq: 6, kind: "service_changed", service: serviceDTO(), running_count: 1 });
  assert.deepEqual([events[0].kind, events[0].request.id, events[0].request.env[0].value, events[0].runningCount], ["service_request", "q1", "", null]);
  assert.equal(events[1].runningCount, 1);
});
