// A session's branches as the row's Branches list knows them, reduced to what a card says.

export interface BoardBranch {
  name: string;
  isSessionBranch: boolean;
  /** "session", "approved", "denied", "deleted" or "unapproved". */
  state: string;
  /** How many pushes of it are recorded. */
  pushes: number;
}

export interface SessionBranches {
  sessionId: string;
  branches: BoardBranch[];
}

/** Adds or replaces the list of one session. */
export function upsertBranches(lists: readonly SessionBranches[], list: SessionBranches): SessionBranches[] {
  return [...lists.filter((l) => l.sessionId !== list.sessionId), list];
}

/** Where the session's own branch stands: "pushed" once a push is recorded, "not pushed" before; "" when it has none. */
export function pushStateOf(list: SessionBranches | undefined): "" | "pushed" | "not pushed" {
  const own = list?.branches.find((b) => b.isSessionBranch);
  if (!own) return "";
  return own.pushes > 0 ? "pushed" : "not pushed";
}

/** Branches the after-turn check found that nobody approved. */
export const unapprovedCount = (list: SessionBranches | undefined): number => list?.branches.filter((b) => b.state === "unapproved").length ?? 0;
