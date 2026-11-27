// The harness's agent system store as the wire shows it, in memory: the fetch of a stub server for the
// settings, repositories and the saves. The adapters' contract suite and the presenters' tests run on it.

import type { FetchLike } from "../../../shared/infrastructure/api-client";

export interface FakeStore {
  readonly fetch: FetchLike;
  /** What the project level sets, by field path. */
  readonly project: Record<string, unknown>;
  /** What one repository sets. */
  repositoryValues(id: string): Record<string, unknown>;
  /** The project's repositories. */
  readonly repositories: { id: string; name: string }[];
  /** Required gate kinds with no command, per repository: it is red. */
  readonly missingGates: Map<string, string[]>;
  /** The run commands of each repository. */
  readonly runCommands: Map<string, { name: string; command: string }[]>;
  /** The project's bench scorecards, newest first, as the bench answers them. */
  scorecards: Record<string, unknown>[];
  /** Every request the store answered, as "METHOD path". */
  readonly requests: string[];
  /** The next save is refused with these errors. */
  refuseNextSave(errors: { field: string; code: string; message: string }[]): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
}

const PROTECTED = ["check.sh", ".github/workflows/**", ".gitlab-ci.yml"];

const DEFAULTS: Record<string, unknown> = {
  "learn.context_map.enabled": true,
  "learn.task_memory.require_update": true,
  "score.trajectory_recording.default": false,
  "guard.protected_paths": PROTECTED,
  "guard.lockfile_rule.enabled": true,
  "guard.retry_limit": 3,
  "guard.tests_fail_first.available": false,
  "review.critic.enabled": true,
  "review.critic.model": "opus",
  "review.critic.standards": ["AGENTS.md", "CLAUDE.md", "context map"],
  "review.visual_qa.ui": null,
};

/** The models the harness can call (domain.ModelChoices). */
const MODELS = ["default", "fable", "opus", "sonnet", "haiku"];

const REQUIRED = ["build", "test", "lint", "secret_scan", "lockfile"];
const OPTIONAL = ["typecheck", "sast", "dependency_scan"];
/** The commands detection fills, per kind; an optional kind has none. */
const DETECTED: Record<string, string> = { build: "go build ./...", test: "go test ./...", lint: "go vet ./...", secret_scan: "gitleaks detect --source .", lockfile: "go mod verify" };
const REASONS: Record<string, string> = { build: "detected from go.mod", test: "detected from go.mod", lint: "detected from go.mod", lockfile: "go.sum present", secret_scan: "gitleaks not installed" };

const GATE_PATH = /^guard\.gates\.([a-z_]+)\.(command|timeout_seconds|enabled)$/;

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

export function fakeStore(projectId = "p1"): FakeStore {
  const project: Record<string, unknown> = {};
  const repos = new Map<string, Record<string, unknown>>();
  const repositories: { id: string; name: string }[] = [];
  const missingGates = new Map<string, string[]>();
  const runCommands = new Map<string, { name: string; command: string }[]>();
  let scorecards: Record<string, unknown>[] = [];
  const requests: string[] = [];
  let refusal: { field: string; code: string; message: string }[] | null = null;
  let down = false;

  const levels = (repo: string | undefined) => (repo === undefined ? [["project", project] as const] : [["repository", repos.get(repo) ?? {}] as const, ["project", project] as const]);
  const field = (repo: string | undefined, path: string) => {
    for (const [source, values] of levels(repo)) if (path in values) return { value: values[path], source };
    return { value: DEFAULTS[path], source: "harness" };
  };
  const gate = (repo: string | undefined, kind: string) => {
    const missing = repo !== undefined && (missingGates.get(repo) ?? []).includes(kind);
    const cmd = field(repo, `guard.gates.${kind}.command`);
    const command = cmd.source === "harness" ? { value: missing ? "" : (DETECTED[kind] ?? ""), source: "harness", reason: REASONS[kind] ?? "" } : cmd;
    const tmo = field(repo, `guard.gates.${kind}.timeout_seconds`);
    const timeout = tmo.source === "harness" ? { value: 600, source: "harness" } : tmo;
    const enabled = field(repo, `guard.gates.${kind}.enabled`);
    return { kind, command, timeout_seconds: timeout, on: enabled.source === "harness" || enabled.value === true };
  };
  const gates = (repo: string | undefined) => [...REQUIRED.map((k) => gate(repo, k)), ...OPTIONAL.map((k) => gate(repo, k)).filter((g) => g.command.value !== "" && g.on)].map((g) => ({ kind: g.kind, command: g.command, timeout_seconds: g.timeout_seconds }));
  const view = (repo: string | undefined) => ({
    effective: {
      learn: { context_map: { enabled: field(repo, "learn.context_map.enabled") }, task_memory: { require_update: field(repo, "learn.task_memory.require_update") } },
      guard: {
        gates: gates(repo),
        protected_paths: field(repo, "guard.protected_paths"),
        lockfile_rule: { enabled: field(repo, "guard.lockfile_rule.enabled") },
        retry_limit: field(repo, "guard.retry_limit"),
        tests_fail_first: { available: field(repo, "guard.tests_fail_first.available") },
      },
      review: { critic: { enabled: field(repo, "review.critic.enabled"), model: field(repo, "review.critic.model"), standards: field(repo, "review.critic.standards") }, visual_qa: { ui: field(repo, "review.visual_qa.ui") } },
      score: { trajectory_recording: { default: field(repo, "score.trajectory_recording.default") } },
      ...(repo !== undefined && (missingGates.get(repo)?.length ?? 0) > 0 ? { missing_required_gates: missingGates.get(repo) } : {}),
    },
    values: repo === undefined ? project : (repos.get(repo) ?? {}),
  });

  /** What the store refuses about one value, by the contract's codes. */
  const refused = (path: string, value: unknown): { field: string; code: string; message: string } | undefined => {
    const gateField = GATE_PATH.exec(path);
    if (value === null) return gateField === null && !(path in DEFAULTS) ? { field: path, code: "UNKNOWN_FIELD", message: `${path} is not a setting` } : undefined;
    if (gateField !== null) {
      const [, kind = "", attribute] = gateField;
      if (!REQUIRED.includes(kind) && !OPTIONAL.includes(kind)) return { field: path, code: "UNKNOWN_FIELD", message: `${path} is not a setting` };
      if (attribute === "command") return typeof value !== "string" ? { field: path, code: "INVALID_TYPE", message: `${path} takes text` } : value.trim() === "" ? { field: path, code: "REQUIRED_GATE_MISSING", message: "an empty command is refused; reset the field" } : undefined;
      if (attribute === "timeout_seconds") return typeof value !== "number" || !Number.isInteger(value) ? { field: path, code: "INVALID_TYPE", message: `${path} takes a whole number` } : value < 1 ? { field: path, code: "OUT_OF_RANGE", message: "takes 1 or more" } : undefined;
      if (typeof value !== "boolean") return { field: path, code: "INVALID_TYPE", message: `${path} takes true or false` };
      return value === false && REQUIRED.includes(kind) ? { field: path, code: "REQUIRED_GATE_DISABLED", message: `${kind} is required and cannot be disabled` } : undefined;
    }
    if (!(path in DEFAULTS)) return { field: path, code: "UNKNOWN_FIELD", message: `${path} is not a setting` };
    const def = DEFAULTS[path];
    if (path === "review.critic.model") return typeof value !== "string" ? { field: path, code: "INVALID_TYPE", message: `${path} takes text` } : MODELS.includes(value) ? undefined : { field: path, code: "MODEL_UNAVAILABLE", message: `the harness cannot call model "${value}"; it offers ${MODELS.join(", ")}` };
    if (path === "review.visual_qa.ui") {
      const ui = value as { url?: string; routes?: string[]; viewports?: string[] };
      if (typeof value !== "object" || Array.isArray(value)) return { field: path, code: "INVALID_TYPE", message: "takes an object" };
      if (!/^https?:\/\/[^/\s]+/.test(ui.url ?? "")) return { field: `${path}.url`, code: "INVALID_ROUTE", message: "must be an http or https URL" };
      const route = (ui.routes ?? []).findIndex((r) => !r.startsWith("/"));
      if (route >= 0) return { field: `${path}.routes[${route}]`, code: "INVALID_ROUTE", message: "a route is a path starting with /" };
      const viewport = (ui.viewports ?? []).findIndex((v) => !/^[1-9]\d*x[1-9]\d*$/.test(v));
      return viewport >= 0 ? { field: `${path}.viewports[${viewport}]`, code: "INVALID_ROUTE", message: "a viewport is WIDTHxHEIGHT in pixels, e.g. 1280x800" } : undefined;
    }
    if (typeof def === "boolean") return typeof value === "boolean" ? undefined : { field: path, code: "INVALID_TYPE", message: `${path} takes true or false` };
    if (typeof def === "number") {
      if (typeof value !== "number" || !Number.isInteger(value)) return { field: path, code: "INVALID_TYPE", message: `${path} takes a whole number` };
      return value < 1 || value > 10 ? { field: path, code: "OUT_OF_RANGE", message: "takes 1–10" } : undefined;
    }
    if (!Array.isArray(value) || !value.every((v) => typeof v === "string")) return { field: path, code: "INVALID_TYPE", message: `${path} takes a list of text` };
    // The store names the entry: "guard.protected_paths[1]".
    const bad = value.findIndex((g) => g.includes("[") && !g.includes("]"));
    return bad < 0 ? undefined : { field: `${path}[${bad}]`, code: "INVALID_GLOB", message: `not a valid glob: ${String(value[bad])}` };
  };

  const fetcher: FetchLike = async (url, init) => {
    if (down) throw new TypeError("offline");
    const u = new URL(url, "http://harness.test");
    const method = init?.method ?? "GET";
    requests.push(`${method} ${u.pathname}${u.search}`);
    if (u.pathname === "/api/list_repositories") return u.searchParams.get("project_id") === projectId ? json(200, { repositories: repositories.map((r) => ({ ...r, project_id: projectId })) }) : json(404, { error: "no such project", code: "PROJECT_NOT_FOUND" });
    if (u.pathname === "/api/list_repository_run_commands") return json(200, { run_commands: runCommands.get(u.searchParams.get("repository_id") ?? "") ?? [] });
    const bench = /^\/api\/projects\/([^/]+)\/bench\/scorecards$/.exec(u.pathname);
    if (bench) return bench[1] === projectId ? json(200, { scorecards }) : json(404, { error: "no such project", code: "PROJECT_NOT_FOUND" });
    const match = /^\/api\/projects\/([^/]+)(?:\/repositories\/([^/]+))?\/agent-system$/.exec(u.pathname);
    if (!match || match[1] !== projectId) return json(404, { error: "no route", code: "NOT_FOUND" });
    const repo = match[2];
    if (repo !== undefined && !repositories.some((r) => r.id === repo)) return json(404, { error: "no such repository", code: "REPOSITORY_NOT_FOUND" });
    if (method === "GET") return json(200, view(repo));
    if (refusal) {
      const errors = refusal;
      refusal = null;
      return json(422, { errors });
    }
    const patch = JSON.parse(String(init?.body ?? "{}")) as Record<string, unknown>;
    const errors: { field: string; code: string; message: string }[] = [];
    for (const [path, value] of Object.entries(patch)) {
      const e = refused(path, value);
      if (e !== undefined) errors.push(e);
    }
    if (errors.length > 0) return json(422, { errors });
    const target = repo === undefined ? project : (repos.get(repo) ?? {});
    for (const [path, value] of Object.entries(patch)) {
      if (value === null) delete target[path];
      else target[path] = value;
    }
    if (repo !== undefined) repos.set(repo, target);
    return json(200, view(repo));
  };

  return {
    fetch: fetcher,
    project,
    repositoryValues: (id) => {
      const values = repos.get(id) ?? {};
      repos.set(id, values);
      return values;
    },
    repositories,
    missingGates,
    runCommands,
    get scorecards() {
      return scorecards;
    },
    set scorecards(list) {
      scorecards = list;
    },
    requests,
    refuseNextSave: (errors) => void (refusal = errors),
    breakNetwork: () => void (down = true),
  };
}
