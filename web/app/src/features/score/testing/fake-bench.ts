// The harness's bench API as the wire shows it, in memory: the fetch of a stub server for cases, scorecards and a run to start.

import type { FetchLike } from "../../../shared/infrastructure/api-client";
import { wireScorecard, type CardSpec } from "./fixtures";

export interface FakeBench {
  readonly fetch: FetchLike;
  /** The project's cases, as the API writes them. */
  readonly cases: { id: string; name?: string; prompt: string; tools: { name: string; description?: string; result?: string; is_error?: boolean }[]; expect?: { tools?: { tool: string; params?: Record<string, unknown> }[]; ordered?: boolean; reference?: string }; source?: string }[];
  /** The recorded trajectories of the project, as the API writes them. */
  readonly trajectories: { id: string; task_id: string; prompt: string; steps: unknown[]; recorded_at: string }[];
  /** The project's scorecards; the API answers them newest first. */
  readonly scorecards: CardSpec[];
  /** Every request answered, as "METHOD path". */
  readonly requests: string[];
  /** The next run start is refused with this message (400 CASES_EMPTY style). */
  refuseRun(message: string): void;
  /** Every call answers 503 as a harness without the bench does. */
  switchOff(): void;
  /** Every call fails as the network does. */
  breakNetwork(): void;
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

/** The server's validation of a definition: the same codes and field paths. */
function faultsOf(def: Pick<FakeBench["cases"][number], "tools" | "expect" | "prompt">): { field: string; code: string; message: string }[] {
  const faults: { field: string; code: string; message: string }[] = [];
  if (!def.prompt || def.prompt.trim() === "") faults.push({ field: "prompt", code: "REQUIRED", message: "a bench case needs a prompt" });
  const mocked = new Set((def.tools ?? []).map((t) => t.name));
  const tools = def.expect?.tools ?? [];
  if (tools.length === 0 && (def.expect?.reference ?? "").trim() === "") faults.push({ field: "expect", code: "NO_EXPECTATION", message: "expect tools or a reference result" });
  tools.forEach((t, i) => {
    if (!mocked.has(t.tool)) faults.push({ field: `expect.tools[${i}].tool`, code: "UNKNOWN_TOOL", message: `tool ${t.tool} has no mock` });
  });
  return faults;
}

export function fakeBench(): FakeBench {
  const cases: FakeBench["cases"] = [];
  const scorecards: CardSpec[] = [];
  const trajectories: FakeBench["trajectories"] = [];
  const requests: string[] = [];
  let refusal: string | null = null;
  let off = false;
  let down = false;
  let runs = 0;
  const fetch: FetchLike = async (input, init) => {
    const method = init?.method ?? "GET";
    const path = input.replace(/^\/api/, "").split("?")[0] ?? "";
    requests.push(`${method} ${path}`);
    if (down) throw new TypeError("offline");
    if (off) return json(503, { message: "eval bench not configured" });
    if (method === "GET" && path === "/projects/p1/bench/cases") return json(200, { cases: cases.map((c) => ({ project_id: "p1", source: "definition", expect: {}, ...c })) });
    if (method === "GET" && path === "/projects/p1/bench/scorecards") return json(200, { scorecards: [...scorecards].sort((a, b) => b.at.localeCompare(a.at)).map(wireScorecard) });
    if (method === "POST" && path === "/projects/p1/bench/runs") {
      if (refusal !== null) return json(400, { error: refusal, code: "BENCH_NO_CASES" });
      runs += 1;
      return json(202, { id: `run${runs}`, project_id: "p1", status: "running", total: cases.length, done: 0, started_at: "2026-11-26T12:00:00Z" });
    }
    if (method === "GET" && path === "/projects/p1/bench/trajectories") return json(200, { trajectories });
    if (method === "POST" && path === "/projects/p1/bench/cases") {
      const def = JSON.parse(String(init?.body ?? "{}")) as FakeBench["cases"][number];
      const faults = faultsOf(def);
      if (faults.length > 0) return json(422, { errors: faults });
      const created = { ...def, id: `case${cases.length + 1}`, source: "definition" };
      cases.push(created);
      return json(201, { project_id: "p1", ...created });
    }
    if (method === "POST" && path === "/projects/p1/bench/cases/from-trajectory") {
      const { trajectory_id } = JSON.parse(String(init?.body ?? "{}")) as { trajectory_id: string };
      const t = trajectories.find((x) => x.id === trajectory_id);
      if (!t) return json(404, { error: "trajectory not found", code: "BENCH_TRAJECTORY_NOT_FOUND" });
      const created = { id: `case${cases.length + 1}`, name: "", prompt: t.prompt, tools: [{ name: "recorded", result: "ok" }], expect: { tools: [{ tool: "recorded" }] }, source: "trajectory" };
      cases.push(created);
      return json(201, { project_id: "p1", ...created });
    }
    const one = /^\/projects\/p1\/bench\/cases\/([^/]+)$/.exec(path);
    if (one && method === "PATCH") {
      const target = cases.find((c) => c.id === one[1]);
      if (!target) return json(404, { error: "bench case not found", code: "BENCH_CASE_NOT_FOUND" });
      const next = { ...target, expect: (JSON.parse(String(init?.body ?? "{}")) as { expect: FakeBench["cases"][number]["expect"] }).expect };
      const faults = faultsOf(next);
      if (faults.length > 0) return json(422, { errors: faults });
      Object.assign(target, next);
      return json(200, { project_id: "p1", source: "definition", ...target });
    }
    if (one && method === "DELETE") {
      const at = cases.findIndex((c) => c.id === one[1]);
      if (at < 0) return json(404, { error: "bench case not found", code: "BENCH_CASE_NOT_FOUND" });
      cases.splice(at, 1);
      return new Response(null, { status: 204 });
    }
    return json(404, { error: "not found", code: "NOT_FOUND" });
  };
  return {
    fetch,
    cases,
    trajectories,
    scorecards,
    requests,
    refuseRun: (message) => {
      refusal = message;
    },
    switchOff: () => {
      off = true;
    },
    breakNetwork: () => {
      down = true;
    },
  };
}
