import type { Change, ChangeKind } from "../../domain/session-context";
import type { ChangeRow, ChangesGroup, HeadingBase } from "./context-view-model";
import { countLabel, matches } from "./search";

/** One change in words: "Skill beta invoked". */
export function changeText(change: Change): string {
  const text: Record<ChangeKind, string> = {
    entry_added: "New entry stored",
    skill_invoked: `Skill ${change.subject} invoked`,
    tool_first_used: `${change.subject} used for the first time`,
    mcp_connected: `MCP server ${change.subject} connected`,
    mcp_failed: `MCP server ${change.subject} failed`,
  };
  return text[change.kind];
}

export const clockTime = (at: Date, timeZone?: string): string => at.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone });

/** Changes while running, newest first as the server sends them. */
export function changesGroup(heading: HeadingBase, changes: readonly Change[], query: string, timeZone?: string): ChangesGroup {
  const rows = changes.map((c): ChangeRow => ({ time: clockTime(c.at, timeZone), text: changeText(c), note: c.note }));
  const shown = rows.filter((r) => matches(query, r.text, r.note));
  return { ...heading, count: countLabel(query, shown.length, rows.length), noMatch: query !== "" && shown.length === 0, rows: shown };
}
