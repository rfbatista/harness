import type { Entry } from "../../domain/session-context";
import type { ContextLink } from "./context-view-model";

const enc = encodeURIComponent;

/**
 * Where each thing that shaped the context is changed, for the pages that exist:
 * the agent and its MCP servers in the catalog, the project's MCP settings, the
 * token savers, and this session's own Sandbox and History tabs. A target with
 * no page (the instruction files, which need a repository) has no link.
 */
export function contextLinks(entry: Entry, sessionId: string, projectId: string): ContextLink[] {
  const links: ContextLink[] = [];
  if (entry.facts.agent !== "") links.push({ label: `Agent ${entry.facts.agent} in the catalog`, href: `/agents/${enc(entry.facts.agent)}` });
  for (const server of entry.servers) if (server.attached === "agent") links.push({ label: `MCP server ${server.name} in the catalog`, href: `/agents/mcp/${enc(server.name)}` });
  if (projectId !== "" && entry.servers.some((s) => s.attached === "project")) links.push({ label: "Project MCP settings", href: `/projects/${enc(projectId)}/settings` });
  if (entry.servers.some((s) => s.attached === "token_saver")) links.push({ label: "Token savers", href: "/token-savers" });
  links.push({ label: "Sandbox tab", href: `?session=${enc(sessionId)}&tab=sandbox` });
  links.push({ label: "History", href: `?session=${enc(sessionId)}&tab=history` });
  return links;
}
