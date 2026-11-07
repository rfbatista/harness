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
      agent: String(r.started_by?.agent ?? ""),
    },
    where: {
      kind: String(r.where?.kind ?? ""),
      dir: String(r.where?.dir ?? ""),
      branch: String(r.where?.branch ?? ""),
      session_id: String(r.where?.session_id ?? ""),
    },
    sandboxed: r.sandboxed !== false,
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
  };
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
    serviceId: String(e.service_id ?? ""),
    runningCount: Number(e.running_count ?? 0),
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
