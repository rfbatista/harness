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
  /** Every request the store answered, as "METHOD path". */
  readonly requests: string[];
  /** The next save is refused with these errors. */
  refuseNextSave(errors: { field: string; code: string; message: string }[]): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
}

const DEFAULTS: Record<string, boolean> = {
  "learn.context_map.enabled": true,
  "learn.task_memory.require_update": true,
  "score.trajectory_recording.default": false,
};

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

export function fakeStore(projectId = "p1"): FakeStore {
  const project: Record<string, unknown> = {};
  const repos = new Map<string, Record<string, unknown>>();
  const repositories: { id: string; name: string }[] = [];
  const missingGates = new Map<string, string[]>();
  const requests: string[] = [];
  let refusal: { field: string; code: string; message: string }[] | null = null;
  let down = false;

  const levels = (repo: string | undefined) => (repo === undefined ? [["project", project] as const] : [["repository", repos.get(repo) ?? {}] as const, ["project", project] as const]);
  const field = (repo: string | undefined, path: string) => {
    for (const [source, values] of levels(repo)) if (path in values) return { value: values[path], source };
    return { value: DEFAULTS[path], source: "harness" };
  };
  const view = (repo: string | undefined) => ({
    effective: {
      learn: { context_map: { enabled: field(repo, "learn.context_map.enabled") }, task_memory: { require_update: field(repo, "learn.task_memory.require_update") } },
      score: { trajectory_recording: { default: field(repo, "score.trajectory_recording.default") } },
      ...(repo !== undefined && (missingGates.get(repo)?.length ?? 0) > 0 ? { missing_required_gates: missingGates.get(repo) } : {}),
    },
    values: repo === undefined ? project : (repos.get(repo) ?? {}),
  });

  const fetcher: FetchLike = async (url, init) => {
    if (down) throw new TypeError("offline");
    const u = new URL(url, "http://harness.test");
    const method = init?.method ?? "GET";
    requests.push(`${method} ${u.pathname}${u.search}`);
    if (u.pathname === "/api/list_repositories") return u.searchParams.get("project_id") === projectId ? json(200, { repositories: repositories.map((r) => ({ ...r, project_id: projectId })) }) : json(404, { error: "no such project", code: "PROJECT_NOT_FOUND" });
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
      if (!(path in DEFAULTS)) errors.push({ field: path, code: "UNKNOWN_FIELD", message: `${path} is not a setting` });
      else if (value !== null && typeof value !== "boolean") errors.push({ field: path, code: "INVALID_TYPE", message: `${path} takes true or false` });
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
    requests,
    refuseNextSave: (errors) => void (refusal = errors),
    breakNetwork: () => void (down = true),
  };
}
