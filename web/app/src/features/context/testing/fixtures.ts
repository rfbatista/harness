// Contract-shaped JSON for GET /api/sessions/{id}/context (Contract: Harness
// server ↔ Web UI — session context), and the domain values it reads as.

import { SessionContextDto } from "../infrastructure/dto/session-context-dto";
import { toSessionContext } from "../infrastructure/session-context-mapper";

export interface FactsJson {
  agent: string;
  role: string;
  mode: string;
  model: string;
  headless: boolean;
  permission_mode: string;
  sandbox: string;
  repository_visibility: string;
  worktree: string;
  branch: string;
  token_savers: string[];
  claude_version: string;
  approx_tokens: number;
}

export interface PieceJson {
  id: string;
  kind: string;
  title: string;
  source: string;
  path: string;
  version: string;
  bytes: number;
  approx_tokens: number;
  text: string;
  truncated: boolean;
  changed: { from_version: string; to_version: string } | null;
}

export interface ToolJson {
  name: string;
  source: string;
  server: string;
  state: string;
}

export interface ServerJson {
  name: string;
  attached: string;
  attached_reason: string;
  status: string;
  status_reason: string;
  hidden_tools: string[];
}

export interface SkillJson {
  name: string;
  source: string;
  description: string;
}

export interface HookJson {
  event: string;
  matcher: string;
  rule: string;
}

export interface EntryJson {
  id: string;
  kind: string;
  at: string;
  facts: FactsJson;
  instructions: PieceJson[];
  tools: ToolJson[];
  mcp_servers: ServerJson[];
  skills: SkillJson[];
  hooks: HookJson[];
}

export interface UseJson {
  count: number;
  last_at: string;
}

export interface ChangeJson {
  at: string;
  kind: string;
  subject: string;
  detail: string;
}

export interface SessionContextJson {
  session_id: string;
  recorded: boolean;
  entries: EntryJson[];
  usage: { tools: Record<string, UseJson>; skills: Record<string, UseJson> };
  changes: ChangeJson[];
}

export const factsJson = (over: Partial<FactsJson> = {}): FactsJson => ({
  agent: "react-developer",
  role: "delegate",
  mode: "implement",
  model: "claude-sonnet-5-5",
  headless: false,
  permission_mode: "acceptEdits",
  sandbox: "seatbelt",
  repository_visibility: "read all",
  worktree: "/work/harness-1",
  branch: "agent/context-tab-1",
  token_savers: ["rtk"],
  claude_version: "2.1.4",
  approx_tokens: 5200,
  ...over,
});

export const pieceJson = (over: Partial<PieceJson> = {}): PieceJson => ({
  id: "p1",
  kind: "brief",
  title: "Task brief",
  source: "Task · harness",
  path: "",
  version: "",
  bytes: 800,
  approx_tokens: 200,
  text: "Build the Context tab.",
  truncated: false,
  changed: null,
  ...over,
});

export const toolJson = (over: Partial<ToolJson> = {}): ToolJson => ({ name: "Bash", source: "claude_code", server: "", state: "allowed", ...over });

export const serverJson = (over: Partial<ServerJson> = {}): ServerJson => ({
  name: "task",
  attached: "built_in",
  attached_reason: "Every sandboxed session (ADR 0025)",
  status: "unknown",
  status_reason: "claude does not report it in a terminal session",
  hidden_tools: [],
  ...over,
});

export const skillJson = (over: Partial<SkillJson> = {}): SkillJson => ({ name: "task-tdd", source: "mode", description: "Build a ticket test first.", ...over });

export const hookJson = (over: Partial<HookJson> = {}): HookJson => ({ event: "PreToolUse", matcher: "Bash", rule: "Rewrites commands through rtk", ...over });

export const entryJson = (over: Partial<EntryJson> = {}): EntryJson => ({
  id: "e1",
  kind: "start",
  at: "2026-11-23T20:49:00Z",
  facts: factsJson(),
  instructions: [pieceJson()],
  tools: [toolJson()],
  mcp_servers: [serverJson()],
  skills: [skillJson()],
  hooks: [hookJson()],
  ...over,
});

export const contextJson = (over: Partial<SessionContextJson> = {}): SessionContextJson => ({
  session_id: "s1",
  recorded: true,
  entries: [entryJson()],
  usage: { tools: {}, skills: {} },
  changes: [],
  ...over,
});

/** The domain value the JSON reads as. */
export const contextFromJson = (json: SessionContextJson) => toSessionContext(SessionContextDto.fromJSON(json));
