// A ServicesGateway that answers from memory, for the component tests: the
// list the test sets, the feed it drives with `emit` and `status`, and a
// record of what was read.

import { toEvent, toServiceList } from "../infrastructure/dto.js";

/** The API's JSON for a service; override fields with `o`. */
export function serviceDTO(o = {}) {
  return {
    id: "s1", project_id: "p1", name: "api", kind: "command", repository_id: "", export_repository_env: false, env: "",
    command: { line: "make air", subdir: "", ports: [] }, image: null, status: "stopped", run: null,
    created_at: "2026-11-07T10:00:00Z", updated_at: "2026-11-07T10:00:00Z", ...o,
  };
}

/** The API's JSON for a run; override fields with `o`. */
export function runDTO(o = {}) {
  return {
    id: "r1", started_at: "2026-11-07T15:00:00Z", ended_at: "", exit_code: null, error: "",
    started_by: { kind: "person", session_id: "", agent: "" },
    where: { kind: "main", dir: "/repo", branch: "main", session_id: "" }, sandboxed: true, ...o,
  };
}

/** A list_services answer. */
export function listDTO(services = [], o = {}) {
  return { services, seq: 0, runtime: { name: "docker", available: true }, sandbox: "on", ...o };
}

/** @param {{ list?: object }} [o] */
export function fakeServices({ list = listDTO() } = {}) {
  let handlers = null;
  const fake = {
    list,
    listCalls: 0,
    listError: null,
    followed: [],
    unfollowed: 0,
    decodeSeed(seed) {
      return { projectId: seed.project_id, list: seed.list ? toServiceList(seed.list) : null };
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
