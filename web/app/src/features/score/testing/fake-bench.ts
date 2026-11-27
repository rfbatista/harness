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
  /** The runs started (and any a test adds, as another session's): what GET and cancel answer, as the API writes them. */
  readonly runs: { id: string; status: string; total: number; done: number; scorecard_id?: string; error?: string; started_at: string }[];
  /** Every request answered, as "METHOD path". */
  readonly requests: string[];
  /** The next run start is refused with this message (400 CASES_EMPTY style). */
  refuseRun(message: string): void;
  /** Every call answers 503 as a harness without the bench does. */
  switchOff(): void;
  /** Every call fails as the network does. */
  breakNetwork(): void;
}

type Wire = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any -- the wire card is built by wireScorecard and read back here

const GROUPS_OF: Record<string, string[]> = { tool_use: ["right_tool", "valid_params", "right_order"], outcome: ["outcome"], trajectory: ["trajectory"] };
const delta = (before: number, after: number) => ({ before, after, change: after - before });

function groupMean(card: Wire, patterns: string[]): number | null {
  let weight = 0;
  let sum = 0;
  for (const name of patterns) {
    const a = (card["patterns"] as Wire)[name] as { scored: number; mean: number } | undefined;
    if (!a || a.scored === 0) continue;
    weight += a.scored;
    sum += a.mean * a.scored;
  }
  return weight === 0 ? null : sum / weight;
}

function caseMean(c: Wire): number | null {
  if (c["error"]) return 0;
  const ps = [...Object.values(c["tool_use"] as Wire), c["outcome"], c["trajectory"]] as { scored: boolean; score: number }[];
  const scored = ps.filter((p) => p.scored);
  return scored.length === 0 ? null : scored.reduce((s, p) => s + p.score, 0) / scored.length;
}

/** The server's compare: a scorecard against a baseline, per aggregate, group and case (the Go Compare, at the wire). */
function compareWire(prev: Wire, cur: Wire): unknown {
  const groups: Record<string, unknown> = {};
  for (const [name, patterns] of Object.entries(GROUPS_OF)) {
    const p = groupMean(prev, patterns);
    const c = groupMean(cur, patterns);
    if (p !== null || c !== null) groups[name] = delta(p ?? 0, c ?? 0);
  }
  const before = new Map((prev["cases"] as Wire[]).map((c) => [c["case_id"] as string, c]));
  const cases = (cur["cases"] as Wire[]).map((c) => {
    const was = before.get(c["case_id"] as string);
    if (was === undefined) return { case_id: c["case_id"], status: "added" };
    const pm = caseMean(was);
    const cm = caseMean(c);
    const moved = pm !== null && cm !== null && Math.abs(cm - pm) > 1e-9;
    return { case_id: c["case_id"], status: moved ? "changed" : "unchanged", ...(moved ? { mean: delta(pm, cm) } : {}) };
  });
  return {
    previous_id: prev["id"],
    fingerprint_changed: (prev["setup"] as Wire)["fingerprint"] !== (cur["setup"] as Wire)["fingerprint"],
    aggregate: delta((prev["aggregate"] as Wire)["mean"], (cur["aggregate"] as Wire)["mean"]),
    groups,
    cases,
  };
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
  const runList: FakeBench["runs"] = [];
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
      const asked = init?.body ? (JSON.parse(String(init.body)) as { case_ids?: string[] }).case_ids : undefined;
      const unknown = (asked ?? []).filter((id) => !cases.some((c) => c.id === id));
      if (unknown.length > 0) return json(422, { error: `not cases of this project: ${unknown.join(", ")}`, code: "BENCH_CASE_IDS_INVALID" });
      runs += 1;
      const started = { id: `run${runs}`, status: "running", total: asked && asked.length > 0 ? asked.length : cases.length, done: 0, started_at: "2026-11-26T12:00:00Z" };
      runList.push(started);
      return json(202, { project_id: "p1", ...started });
    }
    const aRun = /^\/projects\/p1\/bench\/runs\/([^/]+)(\/cancel)?$/.exec(path);
    if (aRun) {
      const run = runList.find((r) => r.id === aRun[1]);
      if (!run) return json(404, { error: "bench run not found", code: "BENCH_RUN_NOT_FOUND" });
      if (method === "GET" && !aRun[2]) return json(200, { project_id: "p1", ...run });
      if (method === "POST" && aRun[2]) {
        if (run.status !== "running") return json(409, { error: `the bench run is ${run.status}`, code: "BENCH_RUN_NOT_RUNNING" });
        // Like the server: the answer is the run as it was; the end follows on the feed.
        const before = { project_id: "p1", ...run };
        run.status = "cancelled";
        return json(200, before);
      }
    }
    const compared = /^\/projects\/p1\/bench\/scorecards\/([^/]+)\/compare$/.exec(path);
    if (method === "GET" && compared) {
      const specs = [...scorecards].sort((a, b) => b.at.localeCompare(a.at));
      const cur = specs.find((s) => s.id === compared[1]);
      if (!cur) return json(404, { error: "scorecard not found", code: "BENCH_SCORECARD_NOT_FOUND" });
      const wanted = new URL(input, "http://x").searchParams.get("baseline");
      const base = wanted ? specs.find((s) => s.id === wanted) : specs.find((s) => s.id !== cur.id && (s.status ?? "finished") === "finished" && s.at < cur.at);
      if (wanted && !base) return json(404, { error: "scorecard not found", code: "BENCH_SCORECARD_NOT_FOUND" });
      if (!base) return json(404, { error: "there is no earlier scorecard to compare with", code: "BENCH_NO_PREVIOUS_SCORECARD" });
      return json(200, compareWire(wireScorecard(base), wireScorecard(cur)));
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
    runs: runList,
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
