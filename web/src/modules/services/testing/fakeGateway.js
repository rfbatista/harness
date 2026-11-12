// A ServicesGateway that answers from memory, for the component tests: the
// list the test sets, the feed it drives with `emit` and `status`, and a
// record of what was read.

import { toBranchList, toEvent, toLogEvent, toRunVariables, toRunWorktrees, toService, toServiceList, toServiceRequest, toServiceRequests } from "../infrastructure/dto.js";

/** The API's JSON for a service; override fields with `o`. */
export function serviceDTO(o = {}) {
  return {
    id: "s1", project_id: "p1", name: "api", kind: "command", repository_id: "", export_repository_env: false, env: "",
    command: { line: "make air", subdir: "", ports: [] }, image: null, status: "stopped", run: null, created_by: { kind: "person" }, pending_requests: 0,
    created_at: "2026-11-07T10:00:00Z", updated_at: "2026-11-07T10:00:00Z", ...o,
  };
}

/** The API's JSON for a run; override fields with `o`. */
export function runDTO(o = {}) {
  return {
    id: "r1", started_at: "2026-11-07T15:00:00Z", ended_at: "", exit_code: null, error: "",
    started_by: { kind: "person", session_id: "", agent: "" },
    where: { kind: "main", dir: "/repo", branch: "main", session_id: "" }, worktree_key: "", overrides: [], sandboxed: true, ...o,
  };
}

/** The API's JSON for a run worktree; override fields with `o`. */
export function worktreeDTO(o = {}) {
  return { key: "main", kind: "main", branch: "main", dir: "/repo", session_id: "", agent: "", session_status: "", ...o };
}

/** The API's JSON for a branch; override fields with `o`. */
export function branchDTO(o = {}) {
  return { name: "feature/x", remote: false, sha: "abc1234", subject: "Add the thing", committed_at: "2026-11-07T12:00:00Z", ...o };
}

/** The API's JSON for a service request; override fields with `o`. */
export function requestDTO(o = {}) {
  return {
    id: "q1", project_id: "p1", service_id: "s1", service_name: "api", action: "update", session_id: "abcdef12-x", task_id: "t1", agent: "go-developer",
    reason: "", summary: "update command service api", proposed: null, changes: [], env: [], worktree: "", state: "pending", note: "", result_error: "",
    requested_at: "2026-11-07T15:00:00Z", answered_at: "", ...o,
  };
}

/** A list_services answer. */
export function listDTO(services = [], o = {}) {
  return { services, seq: 0, runtime: { name: "docker", available: true }, sandbox: "on", ...o };
}

/** @param {{ list?: object }} [o] */
export function fakeServices({ list = listDTO() } = {}) {
  let handlers = null;
  let logHandlers = null;
  const fake = {
    list,
    listCalls: 0,
    listError: null,
    followed: [],
    unfollowed: 0,
    decodeSeed(seed) {
      return { projectId: seed.project_id, list: seed.list ? toServiceList(seed.list) : null };
    },
    /** Calls made to start, stop and restart, as "start:<id>". */
    actions: [],
    /**
     * What each action answers; a test sets its own. By default the service in the list
     * takes the status the action leads to.
     */
    answer: {
      start: async (id) => ({ service: fake.byId(id, "running"), alreadyRunning: false }),
      stop: async (id) => fake.byId(id, "stopped"),
      restart: async (id) => fake.byId(id, "running"),
    },
    /** The API's JSON for service `id` as it would be in `status`. */
    dtoOf(id, status, run) {
      const dto = fake.list.services.find((s) => s.id === id);
      return { ...dto, status, run: run ?? dto.run ?? runDTO() };
    },
    /** The mapped service `id` as it would be in `status`. */
    byId(id, status, run) {
      return toService(fake.dtoOf(id, status, run));
    },
    /** Create, update and delete: what each answers; a test sets its own. */
    saves: [],
    save: {
      create: async (projectId, input) => toService(serviceDTO({ id: "new", project_id: projectId, name: input.name, kind: input.kind, command: input.command, image: input.image })),
      update: async (id, input) => ({ service: toService(serviceDTO({ id, name: input.name, kind: input.kind, command: input.command, image: input.image })), restartNeeded: false }),
      remove: async () => {},
    },
    async createService(projectId, input) {
      fake.saves.push(["create", projectId, input]);
      return fake.save.create(projectId, input);
    },
    async updateService(id, input) {
      fake.saves.push(["update", id, input]);
      return fake.save.update(id, input);
    },
    async deleteService(id) {
      fake.saves.push(["delete", id]);
      return fake.save.remove(id);
    },
    /** What each start was sent: [service id, run worktree key ("" when none was sent)]. */
    starts: [],
    async startService(id, worktree = "") {
      fake.actions.push(`start:${id}`);
      fake.starts.push([id, worktree]);
      return fake.answer.start(id, worktree);
    },
    /** The run worktrees on offer (the API's JSON); a test sets its own. A listError makes the read fail. */
    worktrees: [worktreeDTO({ key: "main", kind: "main", branch: "main", dir: "/repo" })],
    worktreesError: null,
    worktreeReads: [],
    async listRunWorktrees(id) {
      fake.worktreeReads.push(id);
      if (fake.worktreesError) throw fake.worktreesError;
      return toRunWorktrees({ worktrees: fake.worktrees });
    },
    /** The branches on offer (the API's JSON, local first); a test sets its own. The fake filters by the query as the server does. */
    branches: [],
    branchesTruncated: false,
    branchesError: null,
    /** The queries the branch list was asked with, per call. */
    branchQueries: [],
    async listBranches(id, query = "") {
      fake.branchQueries.push(query);
      if (fake.branchesError) throw fake.branchesError;
      const q = query.toLowerCase();
      return toBranchList({ branches: fake.branches.filter((b) => b.name.toLowerCase().includes(q)), truncated: fake.branchesTruncated });
    },
    /** The service requests on offer (the API's JSON, newest first); the fake filters by the state and service as the server does. */
    requests: [],
    requestsError: null,
    /** What each requests read was asked: [project id, filter]. */
    requestReads: [],
    async listRequests(projectId, filter = {}) {
      fake.requestReads.push([projectId, filter]);
      if (fake.requestsError) throw fake.requestsError;
      const wanted = fake.requests.filter((r) => (!filter.state || r.state === filter.state) && (!filter.serviceId || r.service_id === filter.serviceId));
      return toServiceRequests({ requests: wanted });
    },
    /** What each answer was sent: [request id, decision, note]. */
    answered: [],
    /** What an answer returns (the API's JSON); a test sets its own. By default the request becomes done, or denied. */
    requestAnswer: async (id, decision, note) => {
      const found = fake.requests.find((r) => r.id === id);
      return { ...found, state: decision === "approve" ? "done" : "denied", note: note ?? "", answered_at: "2026-11-07T15:05:00Z" };
    },
    async answerRequest(id, decision, note = "") {
      fake.answered.push([id, decision, note]);
      return toServiceRequest(await fake.requestAnswer(id, decision, note));
    },
    /** The variables read answers (the API's JSON); a test sets its own. */
    variables: { run_id: "", variables: [] },
    variablesError: null,
    variableReads: [],
    async getRunVariables(id) {
      fake.variableReads.push(id);
      if (fake.variablesError) throw fake.variablesError;
      return toRunVariables(fake.variables);
    },
    async stopService(id) {
      fake.actions.push(`stop:${id}`);
      return fake.answer.stop(id);
    },
    async restartService(id) {
      fake.actions.push(`restart:${id}`);
      return fake.answer.restart(id);
    },
    /** The log's read answers; a test sets `logRead` (a function of the offset) or pushes with emitLog. */
    logReads: [],
    logRead: null,
    logFollowed: [],
    logUnfollowed: 0,
    async readLog(serviceId, offset) {
      fake.logReads.push([serviceId, offset]);
      if (fake.logError) throw fake.logError;
      return fake.logRead(offset);
    },
    followLog(serviceId, from, onEvent, onStatus) {
      logHandlers = { onEvent, onStatus };
      fake.logFollowed.push([serviceId, from]);
      return () => { fake.logUnfollowed++; logHandlers = null; };
    },
    /** A chunk or reset of the log feed, in the API's JSON. */
    emitLog(dto) {
      logHandlers?.onEvent(toLogEvent(dto));
    },
    logStatus(s) {
      logHandlers?.onStatus?.(s);
    },
    async listServices(projectId) {
      fake.listCalls++;
      if (fake.listError) throw fake.listError;
      return toServiceList({ ...fake.list, services: fake.list.services.map((s) => ({ ...s, project_id: projectId })) });
    },
    follow(projectId, onEvent, onStatus) {
      handlers = { onEvent, onStatus };
      fake.followed.push(projectId);
      return () => { fake.unfollowed++; handlers = null; };
    },
    /** An event of the services feed, in the API's JSON. */
    emit(dto) {
      handlers?.onEvent(toEvent(dto));
    },
    /** A status of the feed. */
    status(s) {
      handlers?.onStatus?.(s);
    },
  };
  return fake;
}

/** An element standing for the page root, with its seed script. */
export function pageEl(seed) {
  return {
    dataset: { seed: "services-seed" },
    tagName: "MAIN",
    getAttribute: () => "servicesPage",
    ownerDocument: { getElementById: () => ({ textContent: JSON.stringify(seed) }) },
  };
}

/** A read_service_log answer. */
export function logDTO(o = {}) {
  return { text: "", offset: 0, next_offset: 0, first_offset: 0, run_id: "r1", ...o };
}
