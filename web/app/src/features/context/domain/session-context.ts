// A session's context: what the harness handed to claude at each start and
// resume (ADR 0099), and how the session used it since. Values only.

export type Role = "architect" | "delegate" | "";

export interface Facts {
  readonly agent: string;
  readonly role: Role;
  readonly mode: string;
  readonly model: string;
  readonly headless: boolean;
  readonly permissionMode: string;
  readonly sandbox: string;
  readonly repositoryVisibility: string;
  readonly worktree: string;
  readonly branch: string;
  readonly tokenSavers: readonly string[];
  /** "" when unknown. */
  readonly claudeVersion: string;
  readonly approxTokens: number;
}

export type PieceKind = "repository_prompt" | "brief" | "opening_message" | "architecture" | "branch_gate" | "instruction_file" | "ruleset" | "memory" | "mcp_instructions" | "other";

export interface Piece {
  readonly id: string;
  readonly kind: PieceKind;
  readonly title: string;
  readonly source: string;
  readonly path: string;
  readonly version: string;
  readonly bytes: number;
  readonly approxTokens: number;
  /** The first 4096 bytes. */
  readonly text: string;
  readonly truncated: boolean;
  readonly changed: { readonly from: string; readonly to: string } | null;
}

export type ToolState = "allowed" | "on_demand" | "hidden" | "refused";

export interface Tool {
  readonly name: string;
  readonly source: "claude_code" | "mcp";
  /** "" for claude_code. */
  readonly server: string;
  readonly state: ToolState;
}

export type Attached = "built_in" | "token_saver" | "agent" | "project" | "mode";
export type McpStatus = "connected" | "failed" | "unknown";

export interface Server {
  readonly name: string;
  readonly attached: Attached;
  readonly attachedReason: string;
  readonly status: McpStatus;
  readonly statusReason: string;
  readonly hiddenTools: readonly string[];
}

export type SkillSource = "mode" | "repository" | "agent" | "built_in";

export interface Skill {
  readonly name: string;
  readonly source: SkillSource;
  readonly description: string;
}

export interface Hook {
  readonly event: string;
  readonly matcher: string;
  readonly rule: string;
}

export interface Entry {
  readonly id: string;
  readonly kind: "start" | "resume";
  readonly at: Date;
  readonly facts: Facts;
  readonly instructions: readonly Piece[];
  readonly tools: readonly Tool[];
  readonly servers: readonly Server[];
  readonly skills: readonly Skill[];
  readonly hooks: readonly Hook[];
}

export interface Use {
  readonly count: number;
  readonly lastAt: Date;
}

export interface Usage {
  readonly tools: ReadonlyMap<string, Use>;
  readonly skills: ReadonlyMap<string, Use>;
}

export type ChangeKind = "entry_added" | "skill_invoked" | "tool_first_used" | "mcp_connected" | "mcp_failed";

export interface Change {
  readonly at: Date;
  readonly kind: ChangeKind;
  readonly subject: string;
  readonly note: string;
}

export interface SessionContext {
  readonly sessionId: string;
  /** false: the session started before the harness recorded context; there are no entries. */
  readonly recorded: boolean;
  /** Oldest first. */
  readonly entries: readonly Entry[];
  readonly usage: Usage;
  /** Newest first. */
  readonly changes: readonly Change[];
}

export type ChangedReason = "entry_added" | "usage" | "mcp_status";

/** The server says a session's context changed: the tab reads it again. */
export interface ContextChanged {
  readonly sessionId: string;
  readonly reason: ChangedReason;
}
