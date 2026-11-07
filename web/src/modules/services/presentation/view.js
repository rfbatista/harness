// What a service row binds, computed from a service (domain/service.js) and the clock.

import { portLinks, startedByView, statusView, timeFact, whereLabel } from "../domain/service.js";

/** The command line, or the image reference with its arguments. */
export function lineOf(service) {
  if (service.image) return [service.image.ref, ...service.image.args].join(" ");
  return service.command?.line ?? "";
}

/**
 * @param {import("../domain/ports.js").Service} service
 * @param {Date} now
 */
export function serviceRow(service, now) {
  const run = service.run;
  const ports = portLinks(service);
  const startedBy = startedByView(run);
  return {
    id: service.id,
    name: service.name,
    kindLabel: service.kind,
    line: lineOf(service),
    status: statusView(service),
    ports: ports.map((p) => ({ ...p, link: p.href !== "" })),
    hasPorts: ports.length > 0,
    startedBy,
    startedByText: startedBy?.text ?? "",
    where: whereLabel(run),
    time: timeFact(service, now),
    notSandboxed: service.kind === "command" && run !== null && !run.sandboxed,
    reason: service.status === "failed" ? (run?.error ?? "") : "",
    hasReason: service.status === "failed" && (run?.error ?? "") !== "",
  };
}
