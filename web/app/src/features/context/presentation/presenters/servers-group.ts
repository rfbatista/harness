import type { Attached, Server } from "../../domain/session-context";
import type { GroupHeading, ServerRow, ServersGroup } from "./context-view-model";

const ATTACHED_LABEL: Record<Attached, string> = { built_in: "Built in", token_saver: "Token saver", agent: "Agent", project: "Project", mode: "Mode" };
const STATUS_LABEL = { connected: "connected", failed: "failed", unknown: "status unknown" } as const;

export function serversGroup(heading: GroupHeading, servers: readonly Server[], copied: ReadonlySet<string>): ServersGroup {
  const rows = servers.map((s): ServerRow => {
    const failed = s.status === "failed";
    return {
      name: s.name,
      attached: ATTACHED_LABEL[s.attached],
      attachedReason: s.attachedReason,
      status: s.status,
      statusLabel: STATUS_LABEL[s.status],
      reason: s.status === "connected" ? "" : s.statusReason,
      failed,
      canCopy: failed && s.statusReason !== "",
      copied: copied.has(s.name),
    };
  });
  return { ...heading, danger: rows.some((r) => r.failed), rows };
}
