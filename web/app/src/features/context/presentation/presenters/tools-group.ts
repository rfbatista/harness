import type { Server, Tool, ToolState, Usage } from "../../domain/session-context";
import type { HeadingBase, ToolRow, ToolSection, ToolsGroup } from "./context-view-model";
import { countLabel, matches } from "./search";

const STATE_LABEL: Record<ToolState, string> = { allowed: "allowed", on_demand: "on demand", hidden: "hidden", refused: "refused" };

/** Claude Code's tools first, then each MCP server's, in the order the servers were attached. */
export function toolsGroup(heading: HeadingBase, tools: readonly Tool[], servers: readonly Server[], usage: Usage, query: string): ToolsGroup {
  const builtIn = tools.filter((t) => t.source === "claude_code");
  const names = [...servers.map((s) => s.name), ...tools.filter((t) => t.source === "mcp").map((t) => t.server)];
  const sections: ToolSection[] = [];
  if (builtIn.length > 0) sections.push(section("Claude Code", builtIn.map((t) => row(t, t.name, usage))));
  for (const name of [...new Set(names)]) {
    const own = tools.filter((t) => t.source === "mcp" && t.server === name);
    const listed = new Set(own.map((t) => t.name));
    const hidden = (servers.find((s) => s.name === name)?.hiddenTools ?? []).filter((n) => !listed.has(n));
    const rows = [...own.map((t) => row(t, short(t.name, name), usage)), ...hidden.map((n) => row({ name: n, source: "mcp", server: name, state: "hidden" }, short(n, name), usage))];
    if (rows.length > 0) sections.push(section(name, rows));
  }
  const all = sections.flatMap((s) => s.rows).length;
  // A section is searched by its rows and by the server's name, so "github" shows all of github's tools.
  const shown = sections.map((s) => (matches(query, s.title) ? s : { ...s, rows: s.rows.filter((r) => matches(query, r.name, r.fullName, r.stateLabel)) })).filter((s) => s.rows.length > 0);
  const total = shown.reduce((n, s) => n + s.rows.length, 0);
  return { ...heading, count: countLabel(query, total, all), noMatch: query !== "" && total === 0, sections: shown.map((s) => ({ ...s, count: countLabel(query, s.rows.length, sections.find((x) => x.title === s.title)!.rows.length) })) };
}

const section = (title: string, rows: ToolRow[]): ToolSection => ({ title, count: String(rows.length), rows });

const short = (name: string, server: string): string => (name.startsWith(`mcp__${server}__`) ? name.slice(`mcp__${server}__`.length) : name);

function row(tool: Tool, name: string, usage: Usage): ToolRow {
  const use = usage.tools.get(tool.name);
  return { name, fullName: tool.name, state: tool.state, stateLabel: STATE_LABEL[tool.state], used: use ? `used ×${use.count}` : "" };
}
