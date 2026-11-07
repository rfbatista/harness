// What the Services page knows about a service, as pure functions (contract:
// Harness server ↔ Web UI — services). A service here is the module's own
// shape (infrastructure/dto.js maps the API's to it); a run keeps the API's
// field names.

const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** @param {{ status: string }} service */
export const isActive = (service) => service.status === "starting" || service.status === "running";

/** @param {{ status: string }[]} services */
export const runningCount = (services) => services.filter(isActive).length;

const byName = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);

/**
 * The status dot's state, its word, and "attention" when the run exited with a
 * non-zero code. The word always says what the dot shows.
 */
export function statusView(service) {
  switch (service.status) {
    case "starting": return { state: "reconnecting", word: "starting", tone: "" };
    case "running": return { state: "running", word: "running", tone: "" };
    case "exited": {
      const code = service.run?.exit_code ?? 0;
      return { state: "done", word: `exited · code ${code}`, tone: code === 0 ? "" : "attention" };
    }
    case "failed": return { state: "failed", word: "failed", tone: "" };
    default: return { state: "idle", word: "stopped", tone: "" };
  }
}

/**
 * Running (starting included) first, then Not running, each by name; a group
 * with no service is left out.
 */
export function groupServices(services) {
  const sorted = [...services].sort(byName);
  const groups = [
    { key: "running", label: "Running", services: sorted.filter(isActive) },
    { key: "stopped", label: "Not running", services: sorted.filter((s) => !isActive(s)) },
  ];
  return groups.filter((g) => g.services.length > 0);
}

/** "6s", "4m", "2h 14m", "3d": a length of time, two units at most. */
function span(ms) {
  const n = Math.max(0, ms);
  if (n < MINUTE) return `${Math.floor(n / SECOND)}s`;
  if (n < HOUR) return `${Math.floor(n / MINUTE)}m`;
  if (n < DAY) {
    const m = Math.floor((n % HOUR) / MINUTE);
    return m === 0 ? `${Math.floor(n / HOUR)}h` : `${Math.floor(n / HOUR)}h ${m}m`;
  }
  return `${Math.floor(n / DAY)}d`;
}

const ago = (ms) => (ms < MINUTE ? "just now" : `${span(ms)} ago`);

/** The time fact of a row: how long it has been up, how it ended, or that it never ran. */
export function timeFact(service, now) {
  const run = service.run;
  if (!run) return "never run";
  const started = Date.parse(run.started_at);
  if (service.status === "running") return `up ${span(now.getTime() - started)}`;
  if (service.status === "starting") return `starting for ${span(now.getTime() - started)}`;
  const ended = Date.parse(run.ended_at);
  if (Number.isNaN(ended)) return "stopped";
  const endedAgo = ago(now.getTime() - ended);
  if (service.status === "stopped") return `stopped ${endedAgo}`;
  return `ran ${span(ended - started)} · ended ${endedAgo}`;
}

/**
 * The ports of a service as links to localhost. An image's link on the host
 * port and show a different container port; a UDP port is text.
 * @returns {{ text: string, href: string, name: string }[]}
 */
export function portLinks(service) {
  const link = (port, text) => ({
    text,
    href: `http://localhost:${port}`,
    name: `Open localhost:${port} (${service.name})`,
  });
  if (service.image) {
    return service.image.ports.map((p) => {
      if (p.protocol === "udp") return { text: `:${p.host}/udp`, href: "", name: "" };
      return link(p.host, p.host === p.container ? `:${p.host}` : `:${p.host} → ${p.container}`);
    });
  }
  return (service.command?.ports ?? []).map((p) => link(p, `:${p}`));
}

/** Where a run runs, in words; "" with no run. */
export function whereLabel(run) {
  switch (run?.where.kind) {
    case "main": return run.where.branch ? `main checkout @ ${run.where.branch}` : "main checkout";
    case "session": return `worktree · ${run.where.branch}`;
    case "service_folder": return "service folder";
    case "container": return "container";
    default: return "";
  }
}

/** Who started a run; null with no run. `sessionId` is set for an agent's session. */
export function startedByView(run) {
  if (!run) return null;
  const by = run.started_by;
  if (by.kind !== "session") return { text: "started by you", sessionId: "" };
  return { text: `started by ${by.agent} · session ${by.session_id.slice(0, 8)}`, sessionId: by.session_id };
}

/**
 * Applies one event of the services feed to the list (contract section 6). An
 * event at or below the list's seq is already in it. A gap asks for the list
 * to be read again. `applied` says the state changed.
 * @returns {{ state: { seq: number, services: any[] }, reload: boolean, applied: boolean, runningCount: number | null }}
 */
export function applyEvent(state, event) {
  if (event.seq <= state.seq) return { state, reload: false, applied: false, runningCount: null };
  if (event.seq > state.seq + 1) return { state, reload: true, applied: false, runningCount: null };
  let services = state.services;
  if (event.kind === "service_changed" && event.service) {
    services = [...services.filter((s) => s.id !== event.service.id), event.service].sort(byName);
  } else if (event.kind === "service_deleted") {
    services = services.filter((s) => s.id !== event.serviceId);
  }
  return { state: { seq: event.seq, services }, reload: false, applied: true, runningCount: event.runningCount };
}
