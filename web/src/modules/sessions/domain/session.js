// Sessions: the rules that hold however they are shown. Mirrors
// internal/domain/session.go; no I/O, no Alpine, no DOM.

/**
 * @typedef {"starting"|"running"|"thinking"|"idle"|"waiting_approval"|"paused"|"done"|"failed"|"stopped"} SessionStatus
 *
 * @typedef {object} Session
 * @property {string} id
 * @property {string} projectId
 * @property {string} repositoryId      the repository its worktree was cut from; "" for none
 * @property {string} parentSessionId   the session that started it on its task; "" when a person did
 * @property {string} workspaceId       its git worktree; "" when it was not started on a repository
 * @property {string} branch            the branch its worktree holds; "" for none
 * @property {string} workingDir        where it runs: its worktree
 * @property {string} ticketId          the task it works on; "" for none. A task can run several sessions at once.
 * @property {string} task              what the session was asked to do
 * @property {string} agentId           "" when no agent persona is attached
 * @property {""|"architect"|"design"} mode  the role it was started in on top of its agent; "" for none
 * @property {SessionStatus} status
 * @property {number} pendingApprovals  tool calls waiting for the developer
 * @property {string} lastAction        "" until the agent has done something
 * @property {boolean} interactive       claude runs in a terminal (a PTY) rather than over stream-json
 * @property {"server"|"tui"|""} runsOn  where an interactive session's terminal lives; "" when headless
 * @property {string} runnerHost         the machine of a "tui" session
 * @property {Date} updatedAt
 *
 * @typedef {{ kind: "upsert", session: Session } | { kind: "deleted", id: string }} SessionChange
 *
 * @typedef {"needs-you"|"active"|"finished"} SessionGroupKey
 * @typedef {{ key: SessionGroupKey, sessions: Session[] }} SessionGroup
 */

export const Status = Object.freeze({
  STARTING: "starting",
  RUNNING: "running",
  THINKING: "thinking",
  IDLE: "idle",
  WAITING_APPROVAL: "waiting_approval",
  PAUSED: "paused",
  DONE: "done",
  FAILED: "failed",
  STOPPED: "stopped",
});

export const STATUSES = Object.freeze(Object.values(Status));

/** The roles a session can be started in on top of its agent. */
export const MODES = Object.freeze(["", "architect", "design"]);

/** The process behind the session is gone for good (domain.SessionStatus.IsTerminal). */
export const isTerminal = (session) =>
  session.status === Status.DONE || session.status === Status.FAILED || session.status === Status.STOPPED;

/** An agent is working on it right now. */
export const isActive = (session) =>
  session.status === Status.STARTING || session.status === Status.RUNNING || session.status === Status.THINKING;

/**
 * The session is waiting on the developer: an approval, or — for an
 * interactive session at its turn boundary (idle) — the next message.
 */
export const needsYou = (session) =>
  !isTerminal(session) &&
  (session.status === Status.WAITING_APPROVAL || session.status === Status.IDLE || session.pendingApprovals > 0);

/** The terminal of this session can be attached from here: it runs on the server and is alive. */
export const hasLiveTerminal = (session) => session.interactive && session.runsOn === "server" && !isTerminal(session);

/**
 * The list after one change from the feed. The changed session moves to the
 * top. `keep` says which sessions the list holds (a task page keeps its own
 * task's): an upsert of any other session leaves it out, so a session that
 * stops matching drops from the list.
 */
export function applyChange(list, change, keep = () => true) {
  if (change.kind === "deleted") return list.filter((s) => s.id !== change.id);
  const rest = list.filter((s) => s.id !== change.session.id);
  return keep(change.session) ? [change.session, ...rest] : rest;
}

/** The predicate a task's page keeps its sessions with. */
export const ofTask = (ticketId) => (session) => session.ticketId === ticketId;

/** Most recently updated first. Returns a new array. */
export const byRecent = (list) => [...list].sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime());

/**
 * Sessions grouped the way the developer triages them: what needs them,
 * what is running, what is over. Empty groups are left out.
 * @returns {SessionGroup[]}
 */
export function group(list) {
  const groups = { "needs-you": [], active: [], finished: [] };
  for (const s of byRecent(list)) {
    if (needsYou(s)) groups["needs-you"].push(s);
    else if (isTerminal(s)) groups.finished.push(s);
    else groups.active.push(s);
  }
  return Object.entries(groups)
    .filter(([, sessions]) => sessions.length > 0)
    .map(([key, sessions]) => ({ key, sessions }));
}
