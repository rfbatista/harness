import type { Attached, Server } from "../../domain/session-context";
import type { HeadingBase, ServerRow, ServersGroup } from "./context-view-model";
import { countLabel, matches } from "./search";

const ATTACHED_LABEL: Record<Attached, string> = { built_in: "Built in", token_saver: "Token saver", agent: "Agent", project: "Project", mode: "Mode" };
const STATUS_LABEL = { connected: "connected", failed: "failed", unknown: "status unknown" } as const;

const UNKNOWN_REASON = "claude does not report it in a terminal session";

export function serversGroup(heading: HeadingBase, servers: readonly Server[], copied: ReadonlySet<string>, query: string, rings: ReadonlySet<string>): ServersGroup {
  const rows = servers.map((s): ServerRow => {
    const failed = s.status === "failed";
    return {
      name: s.name,
      attached: ATTACHED_LABEL[s.attached],
      attachedReason: s.attachedReason,
      status: s.status,
      statusLabel: STATUS_LABEL[s.status],
      reason: s.status === "connected" ? "" : s.status === "unknown" && s.statusReason === "" ? UNKNOWN_REASON : s.statusReason,
      failed,
      canCopy: failed && s.statusReason !== "",
      copied: copied.has(s.name),
      ringing: rings.has(`server:${s.name}`),
    };
  });
  const shown = rows.filter((r) => matches(query, r.name, r.attached, r.attachedReason, r.statusLabel));
  // The failure count stays the group's, whatever the search hides.
  return { ...heading, count: countLabel(query, shown.length, rows.length), noMatch: query !== "" && shown.length === 0, danger: rows.some((r) => r.failed), rows: shown };
}
