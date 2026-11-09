// What a service row binds, computed from a service (domain/service.js) and the clock.

import { MAIN, hasRunIn, runInOptions } from "../domain/runIn.js";
import { isActive, overrideMark, portLinks, startedByView, statusView, timeFact, whereLabel } from "../domain/service.js";

/** The command line, or the image reference with its arguments. */
export function lineOf(service) {
  if (service.image) return [service.image.ref, ...service.image.args].join(" ");
  return service.command?.line ?? "";
}

const FLIGHT_TEXT = { start: "Starting…", stop: "Stopping…", restart: "Restarting…" };

/**
 * One action button of a row. Only the row whose own action is in flight is
 * disabled, and its pressed button says what is happening.
 */
function action(verb, label, service, { show, flight, blocked = "" }) {
  const own = flight === verb;
  return {
    show,
    text: own ? FLIGHT_TEXT[verb] : label,
    name: `${label} ${service.name}`,
    busy: own,
    disabled: flight !== "" || blocked !== "",
    title: blocked,
  };
}

/**
 * @param {import("../domain/ports.js").Service} service
 * @param {Date} now
 * @param {{ flight?: string, runtime?: { name: string, available: boolean } }} [o]
 *   flight: the action of this row in flight ("start", "stop", "restart" or "")
 */
export function serviceRow(service, now, { flight = "", runtime = { name: "docker", available: true } } = {}) {
  const run = service.run;
  const ports = portLinks(service);
  const startedBy = startedByView(run, service.projectId);
  const active = isActive(service);
  // An image service cannot start without its runtime. A command service stays enabled
  // when the sandbox is unavailable: the server refuses and the banner says why.
  const blocked = service.kind === "image" && !runtime.available ? `${runtime.name} was not found. Image services cannot start.` : "";
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
    startedByHref: startedBy?.href ?? "",
    startedByLinked: (startedBy?.href ?? "") !== "",
    startedByPlain: startedBy !== null && startedBy.href === "",
    where: whereLabel(run),
    time: timeFact(service, now),
    overrides: overrideMark(run),
    notSandboxed: service.kind === "command" && run !== null && !run.sandboxed,
    start: action("start", "Start", service, { show: !active, flight, blocked }),
    stop: action("stop", "Stop", service, { show: active, flight }),
    restart: { ...action("restart", "Restart", service, { show: service.status === "running", flight }), title: "Repeats the run: the same worktree and the same overrides" },
    logName: `Log of ${service.name}`,
    moreName: `More actions for ${service.name}`,
    reason: service.status === "failed" ? (run?.error ?? "") : "",
    hasReason: service.status === "failed" && (run?.error ?? "") !== "",
  };
}

const NO_RUN_IN = { show: false, main: [], sessions: [], hasSessions: false, pickedKey: MAIN, selectId: "", loading: null, error: null, fallback: "", hasFallback: false, groupLabel: "", name: "" };

/**
 * The "Run in" picker of a row. It sits beside Start, so it shows with Start: a command service with a
 * repository that is not running. `pick` is the page's state for the service (domain/runIn.js).
 */
export function runInView(service, pick) {
  if (!pick || !hasRunIn(service)) return NO_RUN_IN;
  const options = runInOptions(pick);
  return {
    show: !isActive(service),
    main: options.main,
    sessions: options.sessions,
    hasSessions: options.sessions.length > 0,
    pickedKey: pick.pickedKey,
    loading: pick.state === "loading" ? "true" : null,
    error: pick.state === "failed" ? "Could not load worktrees." : null,
    fallback: pick.fallback?.message ?? "",
    hasFallback: pick.fallback !== null,
    groupLabel: "Other sessions",
    name: `Run ${service.name} in`,
    selectId: `run-in-${service.id}`,
  };
}
