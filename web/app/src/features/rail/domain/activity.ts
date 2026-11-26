// What the rail shows per task: how many of its sessions are live and whether
// one waits on the person. The rules are the server's sessions.IsLive and
// sessions.NeedsYou (internal/adapter/in/web/sessions/view.go).

/** A session as the rail needs it. */
export interface RailSession {
  readonly id: string;
  readonly ticketId: string;
  readonly status: string;
  readonly pendingApprovals: number;
  readonly openInterviewCount: number;
}

export interface Activity {
  readonly live: number;
  readonly attention: boolean;
}

/** One `session` change of the project feed. */
export type SessionChange = { readonly kind: "upsert"; readonly session: RailSession } | { readonly kind: "deleted"; readonly id: string };

const ENDED = new Set(["done", "stopped", "failed"]);

export const isLive = (s: RailSession): boolean => !ENDED.has(s.status);

export const sessionNeedsYou = (s: RailSession): boolean => isLive(s) && (s.status === "waiting_approval" || s.status === "idle" || s.pendingApprovals > 0 || s.openInterviewCount > 0);

/** By task id; sessions without a task are left out. */
export function activityByTask(sessions: readonly RailSession[]): Record<string, Activity> {
  const out: Record<string, { live: number; attention: boolean }> = {};
  for (const s of sessions) {
    if (!s.ticketId) continue;
    const a = (out[s.ticketId] ??= { live: 0, attention: false });
    if (isLive(s)) a.live++;
    a.attention ||= sessionNeedsYou(s);
  }
  return out;
}

/** Applies one feed change to the rail's sessions. */
export function applySessionChange(sessions: readonly RailSession[], change: SessionChange): RailSession[] {
  const rest = sessions.filter((s) => s.id !== (change.kind === "deleted" ? change.id : change.session.id));
  return change.kind === "deleted" ? rest : [...rest, change.session];
}
