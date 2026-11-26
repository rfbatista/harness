// What the task services panel knows about a service, as pure functions
// (Glossary: serviço da task, falha de serviço). The shapes are the panel's own;
// infrastructure maps the API's into them.

export type ServiceStatus = "stopped" | "starting" | "running" | "exited" | "failed";

export interface Credential {
  readonly label: string;
  readonly value: string;
}

export interface Run {
  readonly startedAt: string;
  readonly endedAt: string;
  readonly exitCode: number | null;
  readonly error: string;
  readonly startedBy: { readonly kind: "person" | "session"; readonly taskId: string; readonly agent: string };
}

export interface ImagePort {
  readonly host: number;
  readonly container: number;
  readonly protocol: "tcp" | "udp";
}

export interface Service {
  readonly id: string;
  readonly name: string;
  readonly status: ServiceStatus;
  readonly command: { readonly line: string; readonly ports: readonly number[] } | null;
  readonly image: { readonly ref: string; readonly ports: readonly ImagePort[] } | null;
  readonly run: Run | null;
  readonly createdBy: { readonly kind: "person" | "session"; readonly taskId: string; readonly agent: string };
  readonly credentials: readonly Credential[];
}

/** A serviço da task: a session of the task created it, or started its current or last run. */
export const belongsToTask = (service: Pick<Service, "createdBy" | "run">, taskId: string): boolean =>
  taskId !== "" && (service.createdBy.taskId === taskId || service.run?.startedBy.taskId === taskId);

/** Falha de serviço: failed, or exited with a non-zero code. Exited with 0 is a stopped service. */
export const isFalha = (service: Pick<Service, "status" | "run">): boolean =>
  service.status === "failed" || (service.status === "exited" && (service.run?.exitCode ?? 0) !== 0);

export const isActive = (service: Pick<Service, "status">): boolean => service.status === "starting" || service.status === "running";

export const neverRan = (service: Pick<Service, "status" | "run">): boolean => service.status === "stopped" && service.run === null;

export type Dot = "created" | "running" | "starting" | "stopped" | "failed" | "pending";
export type GroupKey = "attention" | "running" | "stopped";

export interface StatusView {
  readonly dot: Dot;
  /** The word beside the dot, without the command or image. */
  readonly word: string;
}

export function statusView(service: Service): StatusView {
  if (isFalha(service)) {
    const code = service.run?.exitCode;
    return { dot: "failed", word: service.status === "exited" && code !== null && code !== undefined ? `failed · exit code ${code}` : "failed" };
  }
  switch (service.status) {
    case "starting":
      return { dot: "starting", word: "starting" };
    case "running":
      return { dot: "running", word: "running" };
    case "exited":
      return { dot: "stopped", word: "stopped · exit code 0" };
    default:
      return neverRan(service) ? { dot: "created", word: "created · never started" } : { dot: "stopped", word: "stopped by you" };
  }
}

/** The command line or the image, the second line of a row. */
export const whatItRuns = (service: Service): string => service.command?.line ?? service.image?.ref ?? "";

export interface PortLink {
  readonly text: string;
  /** "" for a port that is not a link (UDP). */
  readonly href: string;
  readonly name: string;
}

/** The ports of a service as links to localhost: the Services page's rule. An image links its host port; a UDP port is text. */
export function portLinks(service: Service): PortLink[] {
  const link = (port: number, text: string): PortLink => ({ text, href: `http://localhost:${port}`, name: `Open localhost:${port} (${service.name})` });
  if (service.image) {
    return service.image.ports.map((p) => (p.protocol === "udp" ? { text: `:${p.host}/udp`, href: "", name: "" } : link(p.host, p.host === p.container ? `:${p.host}` : `:${p.host} → ${p.container}`)));
  }
  return (service.command?.ports ?? []).map((p) => link(p, `:${p}`));
}

const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "6s", "4m", "2h 14m", "3d": a length of time, two units at most. */
export function span(ms: number): string {
  const n = Math.max(0, ms);
  if (n < MINUTE) return `${Math.floor(n / SECOND)}s`;
  if (n < HOUR) return `${Math.floor(n / MINUTE)}m`;
  if (n < DAY) {
    const m = Math.floor((n % HOUR) / MINUTE);
    return m === 0 ? `${Math.floor(n / HOUR)}h` : `${Math.floor(n / HOUR)}h ${m}m`;
  }
  return `${Math.floor(n / DAY)}d`;
}

/** The time fact of a row: how long it has been up, when it ended, or that it never ran. */
export function timeFact(service: Service, now: number): string {
  const run = service.run;
  if (!run) return "never run";
  const started = Date.parse(run.startedAt);
  if (service.status === "running" || service.status === "starting") return `up ${span(now - started)}`;
  const ended = Date.parse(run.endedAt);
  return Number.isNaN(ended) ? "stopped" : `ended ${now - ended < MINUTE ? "just now" : `${span(now - ended)} ago`}`;
}

const byName = (a: Service, b: Service): number => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);

export interface Group {
  readonly key: GroupKey;
  readonly label: string;
  readonly services: readonly Service[];
}

/** Needs attention, Running, then Not running, each by name; a group with no service is left out. */
export function groupServices(services: readonly Service[]): Group[] {
  const sorted = [...services].sort(byName);
  return (
    [
      { key: "attention", label: "Needs attention", services: sorted.filter(isFalha) },
      { key: "running", label: "Running", services: sorted.filter((s) => !isFalha(s) && isActive(s)) },
      { key: "stopped", label: "Not running", services: sorted.filter((s) => !isFalha(s) && !isActive(s)) },
    ] satisfies Group[]
  ).filter((g) => g.services.length > 0);
}

/** The first line of a run's error, for a failed row. */
export const firstErrorLine = (service: Service): string => (service.run?.error ?? "").split("\n")[0]?.trim() ?? "";
