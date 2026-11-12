// The API's JSON, mapped to the services module's shapes. A shape that is not
// what the contract says is a BAD_RESPONSE, not a half-drawn page.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const bad = (what) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected services data: ${what}.`);
};
const KINDS = ["command", "image"];
const STATUSES = ["stopped", "starting", "running", "exited", "failed"];
const array = (v) => (Array.isArray(v) ? v : []);

function toRun(r) {
  if (!r) return null;
  return {
    id: String(r.id ?? ""),
    started_at: String(r.started_at ?? ""),
    ended_at: String(r.ended_at ?? ""),
    exit_code: Number.isInteger(r.exit_code) ? r.exit_code : null,
    error: String(r.error ?? ""),
    started_by: {
      kind: r.started_by?.kind === "session" ? "session" : "person",
      session_id: String(r.started_by?.session_id ?? ""),
      task_id: String(r.started_by?.task_id ?? ""),
      agent: String(r.started_by?.agent ?? ""),
    },
    where: {
      kind: String(r.where?.kind ?? ""),
      dir: String(r.where?.dir ?? ""),
      branch: String(r.where?.branch ?? ""),
      session_id: String(r.where?.session_id ?? ""),
      sha: String(r.where?.sha ?? ""),
      remote: r.where?.remote === true,
    },
    worktree_key: String(r.worktree_key ?? ""),
    overrides: array(r.overrides).map((o) => ({ name: String(o?.name ?? ""), by: String(o?.by ?? "") })),
    sandboxed: r.sandboxed !== false,
  };
}

/** @returns {import("../domain/ports.js").BranchList} */
export function toBranchList(body) {
  if (!body || !Array.isArray(body.branches)) bad("expected {branches: […], truncated}");
  return {
    branches: body.branches.map((b) => {
      if (typeof b?.name !== "string" || b.name === "") bad("a branch without a name");
      return { name: b.name, remote: b.remote === true, sha: String(b.sha ?? ""), subject: String(b.subject ?? ""), committedAt: String(b.committed_at ?? "") };
    }),
    truncated: body.truncated === true,
  };
}

const SOURCES = ["repository_env", "service", "override"];

/** @returns {import("../domain/ports.js").RunWorktree[]} */
export function toRunWorktrees(body) {
  if (!body || !Array.isArray(body.worktrees)) bad("expected {worktrees: […]}");
  return body.worktrees.map((w) => {
    if (typeof w?.key !== "string" || w.key === "") bad("a run worktree without a key");
    return Object.freeze({
      key: w.key,
      kind: w.kind === "main" ? "main" : "session",
      branch: String(w.branch ?? ""),
      dir: String(w.dir ?? ""),
      sessionId: String(w.session_id ?? ""),
      agent: String(w.agent ?? ""),
      sessionStatus: String(w.session_status ?? ""),
    });
  });
}

/** @returns {import("../domain/ports.js").RunVariables} */
export function toRunVariables(body) {
  if (!body || !Array.isArray(body.variables)) bad("expected {run_id, variables: […]}");
  return {
    runId: String(body.run_id ?? ""),
    variables: body.variables.map((v) => {
      if (typeof v?.name !== "string" || !SOURCES.includes(v.source)) bad(`a run variable ${JSON.stringify(v?.name)} with source ${JSON.stringify(v?.source)}`);
      return { name: v.name, value: String(v.value ?? ""), source: v.source, by: String(v.by ?? "") };
    }),
  };
}

/** @returns {import("../domain/ports.js").Service} */
export function toService(s) {
  if (!s || typeof s.id !== "string" || typeof s.name !== "string") bad("expected a service with an id and a name");
  if (!KINDS.includes(s.kind)) bad(`service kind ${JSON.stringify(s.kind)}`);
  if (!STATUSES.includes(s.status)) bad(`service status ${JSON.stringify(s.status)}`);
  return {
    id: s.id,
    projectId: String(s.project_id ?? ""),
    name: s.name,
    kind: s.kind,
    repositoryId: String(s.repository_id ?? ""),
    exportRepositoryEnv: s.export_repository_env === true,
    env: String(s.env ?? ""),
    command: s.command ? { line: String(s.command.line ?? ""), subdir: String(s.command.subdir ?? ""), ports: array(s.command.ports).map(Number) } : null,
    image: s.image
      ? {
        ref: String(s.image.ref ?? ""),
        args: array(s.image.args).map(String),
        ports: array(s.image.ports).map((p) => ({ host: Number(p?.host ?? 0), container: Number(p?.container ?? 0), protocol: p?.protocol === "udp" ? "udp" : "tcp" })),
      }
      : null,
    status: s.status,
    run: toRun(s.run),
    createdBy: {
      kind: s.created_by?.kind === "session" ? "session" : "person",
      sessionId: String(s.created_by?.session_id ?? ""),
      taskId: String(s.created_by?.task_id ?? ""),
      agent: String(s.created_by?.agent ?? ""),
    },
    pendingRequests: Number.isInteger(s.pending_requests) ? s.pending_requests : 0,
  };
}

const ACTIONS = ["create", "update", "start", "restart", "stop"];
const REQUEST_STATES = ["pending", "approved", "denied", "expired", "done", "failed"];

/** @returns {import("../domain/ports.js").ServiceRequest} */
export function toServiceRequest(r) {
  if (!r || typeof r.id !== "string" || r.id === "") bad("expected a service request with an id");
  if (!ACTIONS.includes(r.action)) bad(`service request action ${JSON.stringify(r.action)}`);
  if (!REQUEST_STATES.includes(r.state)) bad(`service request state ${JSON.stringify(r.state)}`);
  return {
    id: r.id,
    projectId: String(r.project_id ?? ""),
    serviceId: String(r.service_id ?? ""),
    serviceName: String(r.service_name ?? ""),
    action: r.action,
    sessionId: String(r.session_id ?? ""),
    taskId: String(r.task_id ?? ""),
    agent: String(r.agent ?? ""),
    reason: String(r.reason ?? ""),
    summary: String(r.summary ?? ""),
    proposed: r.proposed && typeof r.proposed === "object" ? r.proposed : null,
    changes: array(r.changes).map((c) => ({ field: String(c?.field ?? ""), before: String(c?.before ?? ""), after: String(c?.after ?? "") })),
    env: array(r.env).map((e) => ({ name: String(e?.name ?? ""), value: String(e?.value ?? ""), change: e?.change === "unset" ? "unset" : "set", previousSet: e?.previous_set === true })),
    worktree: String(r.worktree ?? ""),
    state: r.state,
    note: String(r.note ?? ""),
    resultError: String(r.result_error ?? ""),
    requestedAt: String(r.requested_at ?? ""),
    answeredAt: String(r.answered_at ?? ""),
  };
}

/** @returns {import("../domain/ports.js").ServiceRequest[]} */
export function toServiceRequests(body) {
  if (!body || !Array.isArray(body.requests)) bad("expected {requests: […]}");
  return body.requests.map(toServiceRequest);
}

/** @returns {import("../domain/ports.js").ServiceList} */
export function toServiceList(body) {
  if (!body || !Array.isArray(body.services)) bad("expected {services: […]}");
  return {
    services: body.services.map(toService),
    seq: Number(body.seq ?? 0),
    runtime: { name: body.runtime?.name === "podman" ? "podman" : "docker", available: body.runtime?.available !== false },
    sandbox: ["on", "off", "unavailable"].includes(body.sandbox) ? body.sandbox : "on",
  };
}

/** The page's seed: {project_id, list} where list is a list_services answer, or null when the server could not read it. */
export function toSeed(seed) {
  if (!seed || typeof seed.project_id !== "string") bad("expected a seed with a project_id");
  return { projectId: seed.project_id, list: seed.list ? toServiceList(seed.list) : null };
}

/** @returns {import("../domain/ports.js").ServiceEvent} */
export function toEvent(e) {
  if (!e || !Number.isInteger(e.seq)) bad("expected an event with a seq");
  return {
    seq: e.seq,
    kind: String(e.kind ?? ""),
    service: e.kind === "service_changed" ? toService(e.service) : null,
    request: e.kind === "service_request" ? toServiceRequest(e.request) : null,
    serviceId: String(e.service_id ?? ""),
    runningCount: Number.isInteger(e.running_count) ? e.running_count : (e.kind === "service_request" ? null : Number(e.running_count ?? 0)),
  };
}

/** @returns {import("../domain/ports.js").LogRead} */
export function toLogRead(body) {
  if (!body || typeof body.text !== "string") bad("expected a log read with text");
  return {
    text: body.text,
    offset: Number(body.offset ?? 0),
    next_offset: Number(body.next_offset ?? 0),
    first_offset: Number(body.first_offset ?? 0),
    run_id: String(body.run_id ?? ""),
  };
}

/** @returns {{ kind: "chunk" | "reset", run_id: string, offset: number, text: string }} */
export function toLogEvent(e) {
  if (e?.kind !== "chunk" && e?.kind !== "reset") bad("expected a log event of kind chunk or reset");
  return { kind: e.kind, run_id: String(e.run_id ?? ""), offset: Number(e.offset ?? 0), text: String(e.text ?? "") };
}
