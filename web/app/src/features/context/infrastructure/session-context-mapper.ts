// The session context DTO as domain values.

import type { Change, Entry, Facts, Hook, Piece, Server, SessionContext, Skill, Tool, Usage, Use } from "../domain/session-context";
import type { ChangeDto, EntryDto, FactsDto, HookDto, PieceDto, ServerDto, SessionContextDto, SkillDto, ToolDto, UsageDto, UseDto } from "./dto/session-context-dto";

const facts = (d: FactsDto): Facts => ({
  agent: d.agent,
  role: d.role,
  mode: d.mode,
  model: d.model,
  headless: d.headless,
  permissionMode: d.permission_mode,
  sandbox: d.sandbox,
  repositoryVisibility: d.repository_visibility,
  worktree: d.worktree,
  branch: d.branch,
  tokenSavers: d.token_savers,
  claudeVersion: d.claude_version,
  approxTokens: d.approx_tokens,
});

const piece = (d: PieceDto): Piece => ({
  id: d.id,
  kind: d.kind,
  title: d.title,
  source: d.source,
  path: d.path,
  version: d.version,
  bytes: d.bytes,
  approxTokens: d.approx_tokens,
  text: d.text,
  truncated: d.truncated,
  changed: d.changed === null ? null : { from: d.changed.from_version, to: d.changed.to_version },
});

const tool = (d: ToolDto): Tool => ({ name: d.name, source: d.source, server: d.server, state: d.state });

const server = (d: ServerDto): Server => ({ name: d.name, attached: d.attached, attachedReason: d.attached_reason, status: d.status, statusReason: d.status_reason, hiddenTools: d.hidden_tools });

const skill = (d: SkillDto): Skill => ({ name: d.name, source: d.source, description: d.description });

const hook = (d: HookDto): Hook => ({ event: d.event, matcher: d.matcher, rule: d.rule });

const entry = (d: EntryDto): Entry => ({
  id: d.id,
  kind: d.kind,
  at: new Date(d.at),
  facts: facts(d.facts),
  instructions: d.instructions.map(piece),
  tools: d.tools.map(tool),
  servers: d.mcp_servers.map(server),
  skills: d.skills.map(skill),
  hooks: d.hooks.map(hook),
});

const use = (d: UseDto): Use => ({ count: d.count, lastAt: new Date(d.last_at) });

const uses = (m: Readonly<Record<string, UseDto>>): Map<string, Use> => new Map(Object.entries(m).map(([k, v]) => [k, use(v)]));

const usage = (d: UsageDto): Usage => ({ tools: uses(d.tools), skills: uses(d.skills) });

const change = (d: ChangeDto): Change => ({ at: new Date(d.at), kind: d.kind, subject: d.subject, detail: d.line });

export const toSessionContext = (d: SessionContextDto): SessionContext => ({
  sessionId: d.session_id,
  recorded: d.recorded,
  entries: d.entries.map(entry),
  usage: usage(d.usage),
  changes: d.changes.map(change),
});
