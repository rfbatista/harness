// The memory payloads of the contract "memória da sessão": a session.memory sample, a
// session.memory_killed, and the initial read GET /api/sessions/:id/memory.

import type { BudgetSource, HeaviestProcess, KillSignal, MemoryKill, MemorySample } from "../../kernel/memory/sample";
import { array, count, object, oneOf, optionalTime, string, type Wire } from "./fields";
import { DtoError } from "./dto-error";

const SIGNALS = ["SIGTERM", "SIGKILL"] as const satisfies readonly KillSignal[];
const SOURCES = ["task", "repository", "global"] as const satisfies readonly BudgetSource[];

const heaviest = (from: Wire, dto: string, at: string): HeaviestProcess | null => {
  const raw = from.heaviest;
  if (raw === undefined || raw === null) return null;
  const path = at === "" ? "heaviest" : `${at}.heaviest`;
  const h = object(dto, raw, path);
  return { pid: count(dto, h, "pid", path), command: string(dto, h, "command", path), rssBytes: count(dto, h, "rss_bytes", path) };
};

export class MemorySampleDto {
  constructor(
    readonly session_id: string,
    readonly task_id: string,
    readonly sampled_at: Date | null,
    readonly rss_bytes: number,
    readonly budget_bytes: number,
    readonly processes: number,
    readonly heaviest: HeaviestProcess | null,
  ) {}

  static fromJSON(raw: unknown, at = ""): MemorySampleDto {
    const dto = "MemorySampleDto";
    const from = object(dto, raw, at);
    return new MemorySampleDto(
      string(dto, from, "session_id", at),
      string(dto, from, "task_id", at),
      optionalTime(dto, from, "sampled_at", at),
      count(dto, from, "rss_bytes", at),
      count(dto, from, "budget_bytes", at),
      count(dto, from, "processes", at),
      heaviest(from, dto, at),
    );
  }

  toDomain(): MemorySample {
    return {
      sessionId: this.session_id,
      taskId: this.task_id,
      sampledAt: this.sampled_at,
      rssBytes: this.rss_bytes,
      budgetBytes: this.budget_bytes,
      processes: this.processes,
      heaviest: this.heaviest,
    };
  }
}

export class MemoryKillDto {
  constructor(
    readonly session_id: string,
    readonly task_id: string,
    readonly killed_at: Date,
    readonly pid: number,
    readonly command: string,
    readonly rss_bytes: number,
    readonly tree_rss_bytes: number,
    readonly budget_bytes: number,
    readonly signal: KillSignal,
  ) {}

  static fromJSON(raw: unknown, at = ""): MemoryKillDto {
    const dto = "MemoryKillDto";
    const from = object(dto, raw, at);
    const killedAt = optionalTime(dto, from, "killed_at", at);
    if (killedAt === null) throw new DtoError(dto, at === "" ? "killed_at" : `${at}.killed_at`, "expected a time");
    return new MemoryKillDto(
      string(dto, from, "session_id", at),
      string(dto, from, "task_id", at),
      killedAt,
      count(dto, from, "pid", at),
      string(dto, from, "command", at),
      count(dto, from, "rss_bytes", at),
      count(dto, from, "tree_rss_bytes", at),
      count(dto, from, "budget_bytes", at),
      oneOf(dto, from, "signal", SIGNALS, at),
    );
  }

  toDomain(): MemoryKill {
    return {
      sessionId: this.session_id,
      taskId: this.task_id,
      killedAt: this.killed_at,
      pid: this.pid,
      command: this.command,
      rssBytes: this.rss_bytes,
      treeRssBytes: this.tree_rss_bytes,
      budgetBytes: this.budget_bytes,
      signal: this.signal,
    };
  }
}

export interface Prevention {
  heapMb: number;
  testWorkers: number;
}

/** GET /api/sessions/:id/memory: the last sample plus what the popover explains. */
export class MemoryReadDto {
  constructor(
    readonly sample: MemorySample,
    readonly kills: MemoryKill[],
    readonly budgetSource: BudgetSource,
    readonly prevention: Prevention,
  ) {}

  static fromJSON(raw: unknown): MemoryReadDto {
    const dto = "MemoryReadDto";
    const from = object(dto, raw);
    const prevention = object(dto, from.prevention, "prevention");
    return new MemoryReadDto(
      MemorySampleDto.fromJSON(raw).toDomain(),
      array(dto, from, "kills", (item, at) => MemoryKillDto.fromJSON(item, at).toDomain()),
      oneOf(dto, from, "budget_source", SOURCES),
      { heapMb: count(dto, prevention, "heap_mb", "prevention"), testWorkers: count(dto, prevention, "test_workers", "prevention") },
    );
  }
}
