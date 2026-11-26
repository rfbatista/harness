// Contract payloads of "memória da sessão" as the wire sends them, and the domain values they decode to.

export const GB = 1024 ** 3;
export const MB = 1024 ** 2;
export const SESSION = "e7bb0000-0000-4000-8000-000000000001";
export const TASK = "dc35c9f0d90466ce";

/** A `session.memory` event (contract §1); `over` replaces fields. */
export function wireSample(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    kind: "session.memory",
    session_id: SESSION,
    task_id: TASK,
    sampled_at: "2026-11-25T22:40:01Z",
    rss_bytes: 1288490188,
    budget_bytes: 4 * GB,
    processes: 7,
    heaviest: { pid: 81234, command: "node jest", rss_bytes: 905969664 },
    ...over,
  };
}

/** A `session.memory_killed` event (contract §2). */
export function wireKill(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    kind: "session.memory_killed",
    session_id: SESSION,
    task_id: TASK,
    killed_at: "2026-11-25T22:40:03Z",
    pid: 81234,
    command: "node jest",
    rss_bytes: 3758096384,
    tree_rss_bytes: 4400000000,
    budget_bytes: 4 * GB,
    signal: "SIGTERM",
    ...over,
  };
}

/** The initial read (contract §3). */
export function wireRead(over: Record<string, unknown> = {}): Record<string, unknown> {
  const sample = wireSample();
  delete sample.kind;
  return { ...sample, kills: [], budget_source: "repository", prevention: { heap_mb: 1365, test_workers: 2 }, ...over };
}
