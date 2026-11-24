// The session context as the server sends it (Contract: Harness server ↔ Web
// UI — session context): GET /api/sessions/{id}/context and the full-piece
// endpoint. Fields keep the wire names.

import { array, boolean, count, object, oneOf, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";

export const ROLES = ["architect", "delegate", ""] as const;
export const PIECE_KINDS = ["repository_prompt", "brief", "opening_message", "architecture", "branch_gate", "instruction_file", "ruleset", "memory", "mcp_instructions", "other"] as const;
export const TOOL_STATES = ["allowed", "on_demand", "hidden", "refused"] as const;
export const ATTACHED = ["built_in", "token_saver", "agent", "project", "mode"] as const;
export const MCP_STATUSES = ["connected", "failed", "unknown"] as const;
export const SKILL_SOURCES = ["mode", "repository", "agent", "built_in"] as const;
export const CHANGE_KINDS = ["entry_added", "skill_invoked", "tool_first_used", "mcp_connected", "mcp_failed"] as const;

export class FactsDto {
  constructor(
    readonly agent: string,
    readonly role: (typeof ROLES)[number],
    readonly mode: string,
    readonly model: string,
    readonly headless: boolean,
    readonly permission_mode: string,
    readonly sandbox: string,
    readonly repository_visibility: string,
    readonly worktree: string,
    readonly branch: string,
    readonly token_savers: readonly string[],
    readonly claude_version: string,
    readonly approx_tokens: number,
  ) {}

  static fromJSON(raw: unknown, at = ""): FactsDto {
    const n = "FactsDto";
    const from = object(n, raw, at);
    return new FactsDto(
      string(n, from, "agent", at),
      oneOf(n, from, "role", ROLES, at),
      string(n, from, "mode", at),
      string(n, from, "model", at),
      boolean(n, from, "headless", at),
      string(n, from, "permission_mode", at),
      string(n, from, "sandbox", at),
      string(n, from, "repository_visibility", at),
      string(n, from, "worktree", at),
      string(n, from, "branch", at),
      array(n, from, "token_savers", (item, path) => stringItem(n, item, path), at),
      string(n, from, "claude_version", at),
      count(n, from, "approx_tokens", at),
    );
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class PieceDto {
  constructor(
    readonly id: string,
    readonly kind: (typeof PIECE_KINDS)[number],
    readonly title: string,
    readonly source: string,
    readonly path: string,
    readonly version: string,
    readonly bytes: number,
    readonly approx_tokens: number,
    readonly text: string,
    readonly truncated: boolean,
    readonly changed: { readonly from_version: string; readonly to_version: string } | null,
  ) {}

  static fromJSON(raw: unknown, at = ""): PieceDto {
    const n = "PieceDto";
    const from = object(n, raw, at);
    const changed = from.changed;
    let change: PieceDto["changed"] = null;
    if (changed !== null && changed !== undefined) {
      const c = object(n, changed, `${at}.changed`);
      change = { from_version: string(n, c, "from_version", `${at}.changed`), to_version: string(n, c, "to_version", `${at}.changed`) };
    }
    return new PieceDto(
      string(n, from, "id", at),
      oneOf(n, from, "kind", PIECE_KINDS, at),
      string(n, from, "title", at),
      string(n, from, "source", at),
      string(n, from, "path", at),
      string(n, from, "version", at),
      count(n, from, "bytes", at),
      count(n, from, "approx_tokens", at),
      string(n, from, "text", at),
      boolean(n, from, "truncated", at),
      change,
    );
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class ToolDto {
  constructor(
    readonly name: string,
    readonly source: "claude_code" | "mcp",
    readonly server: string,
    readonly state: (typeof TOOL_STATES)[number],
  ) {}

  static fromJSON(raw: unknown, at = ""): ToolDto {
    const n = "ToolDto";
    const from = object(n, raw, at);
    return new ToolDto(string(n, from, "name", at), oneOf(n, from, "source", ["claude_code", "mcp"], at), string(n, from, "server", at), oneOf(n, from, "state", TOOL_STATES, at));
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class ServerDto {
  constructor(
    readonly name: string,
    readonly attached: (typeof ATTACHED)[number],
    readonly attached_reason: string,
    readonly status: (typeof MCP_STATUSES)[number],
    readonly status_reason: string,
    readonly hidden_tools: readonly string[],
  ) {}

  static fromJSON(raw: unknown, at = ""): ServerDto {
    const n = "ServerDto";
    const from = object(n, raw, at);
    return new ServerDto(
      string(n, from, "name", at),
      oneOf(n, from, "attached", ATTACHED, at),
      string(n, from, "attached_reason", at),
      oneOf(n, from, "status", MCP_STATUSES, at),
      string(n, from, "status_reason", at),
      array(n, from, "hidden_tools", (item, path) => stringItem(n, item, path), at),
    );
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class SkillDto {
  constructor(
    readonly name: string,
    readonly source: (typeof SKILL_SOURCES)[number],
    readonly description: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): SkillDto {
    const n = "SkillDto";
    const from = object(n, raw, at);
    return new SkillDto(string(n, from, "name", at), oneOf(n, from, "source", SKILL_SOURCES, at), string(n, from, "description", at));
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class HookDto {
  constructor(
    readonly event: string,
    readonly matcher: string,
    readonly rule: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): HookDto {
    const n = "HookDto";
    const from = object(n, raw, at);
    return new HookDto(string(n, from, "event", at), string(n, from, "matcher", at), string(n, from, "rule", at));
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class EntryDto {
  constructor(
    readonly id: string,
    readonly kind: "start" | "resume",
    readonly at: string,
    readonly facts: FactsDto,
    readonly instructions: readonly PieceDto[],
    readonly tools: readonly ToolDto[],
    readonly mcp_servers: readonly ServerDto[],
    readonly skills: readonly SkillDto[],
    readonly hooks: readonly HookDto[],
  ) {}

  static fromJSON(raw: unknown, path = ""): EntryDto {
    const n = "EntryDto";
    const from = object(n, raw, path);
    time(n, from, "at", path);
    const sub = (key: string) => (path === "" ? key : `${path}.${key}`);
    return new EntryDto(
      string(n, from, "id", path),
      oneOf(n, from, "kind", ["start", "resume"], path),
      string(n, from, "at", path),
      FactsDto.fromJSON(from.facts, sub("facts")),
      array(n, from, "instructions", (item, at) => PieceDto.fromJSON(item, at), path),
      array(n, from, "tools", (item, at) => ToolDto.fromJSON(item, at), path),
      array(n, from, "mcp_servers", (item, at) => ServerDto.fromJSON(item, at), path),
      array(n, from, "skills", (item, at) => SkillDto.fromJSON(item, at), path),
      array(n, from, "hooks", (item, at) => HookDto.fromJSON(item, at), path),
    );
  }

  toJSON(): Wire {
    return { id: this.id, kind: this.kind, at: this.at, facts: this.facts.toJSON(), instructions: this.instructions.map((p) => p.toJSON()), tools: this.tools.map((t) => t.toJSON()), mcp_servers: this.mcp_servers.map((s) => s.toJSON()), skills: this.skills.map((s) => s.toJSON()), hooks: this.hooks.map((h) => h.toJSON()) };
  }
}

export class UseDto {
  constructor(
    readonly count: number,
    readonly last_at: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): UseDto {
    const n = "UseDto";
    const from = object(n, raw, at);
    time(n, from, "last_at", at);
    return new UseDto(count(n, from, "count", at), string(n, from, "last_at", at));
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class UsageDto {
  constructor(
    readonly tools: Readonly<Record<string, UseDto>>,
    readonly skills: Readonly<Record<string, UseDto>>,
  ) {}

  static fromJSON(raw: unknown, at = ""): UsageDto {
    const n = "UsageDto";
    const from = object(n, raw, at);
    const sub = (key: string) => (at === "" ? key : `${at}.${key}`);
    return new UsageDto(uses(n, from.tools, sub("tools")), uses(n, from.skills, sub("skills")));
  }

  toJSON(): Wire {
    const out = (m: Readonly<Record<string, UseDto>>) => Object.fromEntries(Object.entries(m).map(([k, v]) => [k, v.toJSON()]));
    return { tools: out(this.tools), skills: out(this.skills) };
  }
}

export class ChangeDto {
  constructor(
    readonly at: string,
    readonly kind: (typeof CHANGE_KINDS)[number],
    readonly subject: string,
    readonly detail: string,
  ) {}

  static fromJSON(raw: unknown, path = ""): ChangeDto {
    const n = "ChangeDto";
    const from = object(n, raw, path);
    time(n, from, "at", path);
    return new ChangeDto(string(n, from, "at", path), oneOf(n, from, "kind", CHANGE_KINDS, path), string(n, from, "subject", path), string(n, from, "detail", path));
  }

  /** `detail` under a name the mapper may read: the lint bans `.detail` outside DTOs (it is a DOM event's raw data). */
  get line(): string {
    return this.detail;
  }

  toJSON(): Wire {
    return { ...this } as Wire;
  }
}

export class SessionContextDto {
  constructor(
    readonly session_id: string,
    readonly recorded: boolean,
    readonly entries: readonly EntryDto[],
    readonly usage: UsageDto,
    readonly changes: readonly ChangeDto[],
  ) {}

  static fromJSON(raw: unknown): SessionContextDto {
    const n = "SessionContextDto";
    const from = object(n, raw);
    return new SessionContextDto(
      string(n, from, "session_id"),
      boolean(n, from, "recorded"),
      array(n, from, "entries", (item, at) => EntryDto.fromJSON(item, at)),
      UsageDto.fromJSON(from.usage, "usage"),
      array(n, from, "changes", (item, at) => ChangeDto.fromJSON(item, at)),
    );
  }

  toJSON(): Wire {
    return { session_id: this.session_id, recorded: this.recorded, entries: this.entries.map((e) => e.toJSON()), usage: this.usage.toJSON(), changes: this.changes.map((c) => c.toJSON()) };
  }
}

/** The full-piece endpoint's answer: { text }. */
export class PieceTextDto {
  constructor(readonly text: string) {}

  static fromJSON(raw: unknown): PieceTextDto {
    return new PieceTextDto(string("PieceTextDto", object("PieceTextDto", raw), "text"));
  }

  toJSON(): Wire {
    return { text: this.text };
  }
}

function stringItem(dto: string, item: unknown, at: string): string {
  if (typeof item !== "string") throw new DtoError(dto, at, "expected a string");
  return item;
}

function uses(dto: string, raw: unknown, at: string): Record<string, UseDto> {
  const from = object(dto, raw, at);
  return Object.fromEntries(Object.entries(from).map(([key, value]) => [key, UseDto.fromJSON(value, `${at}.${key}`)]));
}
