// The harness's bench API as the wire shows it, in memory: the fetch of a stub server for cases, scorecards and a run to start.

import type { FetchLike } from "../../../shared/infrastructure/api-client";
import { wireScorecard, type CardSpec } from "./fixtures";

export interface FakeBench {
  readonly fetch: FetchLike;
  /** The project's cases, as the API writes them. */
  readonly cases: { id: string; name?: string; prompt: string; tools: { name: string }[]; source?: string }[];
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

export function fakeBench(): FakeBench {
  const cases: FakeBench["cases"] = [];
  const scorecards: CardSpec[] = [];
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
    return json(404, { error: "not found", code: "NOT_FOUND" });
  };
  return {
    fetch,
    cases,
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
